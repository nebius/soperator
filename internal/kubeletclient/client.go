// Package kubeletclient talks to a node's kubelet directly over its read-only HTTPS API,
// bypassing the API server's nodes/proxy sub-resource. The proxy path puts the API server
// on the critical path of every request and does not scale to thousands of nodes.
package kubeletclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	// DefaultPort is used when a node does not advertise its kubelet endpoint.
	DefaultPort = 10250

	// SummaryPath returns per-pod resource usage, built from cAdvisor and slow on busy nodes.
	SummaryPath = "/stats/summary"
	// PodsPath returns the full pod objects the kubelet is running.
	PodsPath = "/pods"

	dialTimeout         = 5 * time.Second
	tlsHandshakeTimeout = 5 * time.Second
	keepAlive           = 30 * time.Second
	idleConnTimeout     = 5 * time.Minute

	// Kubelet error bodies are short; cap the read so a misbehaving endpoint cannot
	// blow up the log line.
	maxErrorBodyBytes = 2048
)

// Node name is deliberately not a label: at several thousand nodes it would make these
// series unusable.
var (
	requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "soperator_kubelet_requests_total",
		Help: "Requests to the kubelet read-only API, by endpoint and outcome",
	}, []string{"path", "outcome"})

	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "soperator_kubelet_request_duration_seconds",
		Help:    "Latency of requests to the kubelet read-only API, by endpoint",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
	}, []string{"path"})
)

func init() {
	ctrlmetrics.Registry.MustRegister(requestsTotal, requestDuration)
}

// Config configures direct access to a kubelet.
type Config struct {
	// Port is the fallback kubelet port for nodes that do not advertise a kubelet endpoint.
	Port int32
	// Timeout bounds a single request.
	Timeout time.Duration
	// MaxIdleConns caps pooled connections. One connection is kept per node, so this
	// should be at least the number of nodes the caller talks to or every request pays
	// a fresh TLS handshake.
	MaxIdleConns int
	// InsecureSkipTLSVerify skips verification of the kubelet serving certificate, which
	// is self-signed unless the cluster enables serverTLSBootstrap.
	InsecureSkipTLSVerify bool
	CAFile                string
	TLSServerName         string
}

// Client reads from kubelets directly. It authenticates with the caller's service account
// token, so each kubelet authorizes the request against the sub-resource the requested path
// maps to: /stats against nodes/stats, everything else against nodes/proxy.
type Client struct {
	httpClient  *http.Client
	scheme      string
	defaultPort int32
}

func New(restConfig *rest.Config, cfg Config) (*Client, error) {
	tlsConfig, err := transport.TLSConfigFor(&transport.Config{
		TLS: transport.TLSConfig{
			Insecure:   cfg.InsecureSkipTLSVerify,
			CAFile:     cfg.CAFile,
			ServerName: cfg.TLSServerName,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build kubelet TLS config: %w", err)
	}

	var roundTripper http.RoundTripper = &http.Transport{
		TLSClientConfig: tlsConfig,
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: keepAlive,
		}).DialContext,
		TLSHandshakeTimeout: tlsHandshakeTimeout,
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     idleConnTimeout,
	}

	if restConfig != nil && (restConfig.BearerToken != "" || restConfig.BearerTokenFile != "") {
		roundTripper, err = transport.NewBearerAuthWithRefreshRoundTripper(
			restConfig.BearerToken, restConfig.BearerTokenFile, roundTripper,
		)
		if err != nil {
			return nil, fmt.Errorf("build kubelet bearer auth: %w", err)
		}
	}

	port := cfg.Port
	if port == 0 {
		port = DefaultPort
	}

	return &Client{
		httpClient:  &http.Client{Transport: roundTripper, Timeout: cfg.Timeout},
		scheme:      "https",
		defaultPort: port,
	}, nil
}

// DefaultPort reports the port used for requests that do not carry one.
func (c *Client) DefaultPort() int32 {
	return c.defaultPort
}

// Get fetches path from the kubelet listening at address:port and decodes the JSON
// response into out. A zero port falls back to the client default.
func (c *Client) Get(ctx context.Context, address string, port int32, path string, out any) error {
	if port == 0 {
		port = c.defaultPort
	}

	url := fmt.Sprintf("%s://%s%s", c.scheme, net.JoinHostPort(address, strconv.Itoa(int(port))), path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create kubelet request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	started := time.Now()
	resp, err := c.httpClient.Do(req)
	requestDuration.WithLabelValues(path).Observe(time.Since(started).Seconds())
	if err != nil {
		requestsTotal.WithLabelValues(path, "error").Inc()
		return fmt.Errorf("request kubelet %s: %w", path, err)
	}
	defer func() {
		// Drain before closing so the connection returns to the pool.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		requestsTotal.WithLabelValues(path, "error").Inc()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return fmt.Errorf("kubelet %s returned %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		requestsTotal.WithLabelValues(path, "error").Inc()
		return fmt.Errorf("decode kubelet %s response: %w", path, err)
	}

	requestsTotal.WithLabelValues(path, "success").Inc()
	return nil
}

// AddressForNode resolves where a node's kubelet listens, preferring the internal IP
// the same way Prometheus' node service discovery does. A zero port means the node did not
// advertise its kubelet endpoint and the client default applies.
func AddressForNode(node *corev1.Node) (string, int32, error) {
	var hostname string
	for _, addr := range node.Status.Addresses {
		switch addr.Type {
		case corev1.NodeInternalIP:
			if addr.Address != "" {
				return addr.Address, node.Status.DaemonEndpoints.KubeletEndpoint.Port, nil
			}
		case corev1.NodeHostName:
			if hostname == "" {
				hostname = addr.Address
			}
		}
	}

	if hostname == "" {
		return "", 0, fmt.Errorf("resolve kubelet address: node %s has no internal IP or hostname", node.Name)
	}

	return hostname, node.Status.DaemonEndpoints.KubeletEndpoint.Port, nil
}
