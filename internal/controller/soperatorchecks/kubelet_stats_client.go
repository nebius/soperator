package soperatorchecks

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
	// DefaultKubeletPort is used when a node does not advertise its kubelet endpoint.
	DefaultKubeletPort = 10250

	kubeletSummaryPath = "/stats/summary"

	// Kubelet builds the summary by querying cAdvisor, which is slow on busy nodes.
	kubeletDialTimeout         = 5 * time.Second
	kubeletTLSHandshakeTimeout = 5 * time.Second
	kubeletKeepAlive           = 30 * time.Second
	kubeletIdleConnTimeout     = 5 * time.Minute

	// Kubelet error bodies are short; cap the read so a misbehaving endpoint cannot
	// blow up the log line.
	kubeletMaxErrorBodyBytes = 2048
)

// Node name is deliberately not a label: at several thousand nodes it would make these
// series unusable.
var (
	kubeletStatsRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "soperator_kubelet_stats_requests_total",
		Help: "Requests to the kubelet stats endpoint, by outcome",
	}, []string{"outcome"})

	kubeletStatsRequestDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "soperator_kubelet_stats_request_duration_seconds",
		Help:    "Latency of requests to the kubelet stats endpoint",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
	})
)

func init() {
	ctrlmetrics.Registry.MustRegister(kubeletStatsRequestsTotal, kubeletStatsRequestDuration)
}

// KubeletClientConfig configures direct access to the kubelet stats endpoint.
type KubeletClientConfig struct {
	// Port is the fallback kubelet port for nodes that do not advertise a kubelet endpoint.
	Port int32
	// Timeout bounds a single stats request.
	Timeout time.Duration
	// MaxIdleConns caps pooled connections. One connection is kept per node, so this
	// should be at least the node count or every request pays a fresh TLS handshake.
	MaxIdleConns int
	// InsecureSkipTLSVerify skips verification of the kubelet serving certificate, which
	// is self-signed unless the cluster enables serverTLSBootstrap.
	InsecureSkipTLSVerify bool
	CAFile                string
	TLSServerName         string
}

// kubeletStatsClient reads pod stats from each node's kubelet directly rather than through
// the API server's nodes/proxy sub-resource, which does not scale to thousands of nodes.
// It authenticates with the operator's service account token, so the kubelet authorizes
// the request against the nodes/stats sub-resource.
type kubeletStatsClient struct {
	httpClient  *http.Client
	scheme      string
	defaultPort int32
}

func newKubeletStatsClient(restConfig *rest.Config, cfg KubeletClientConfig) (*kubeletStatsClient, error) {
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
			Timeout:   kubeletDialTimeout,
			KeepAlive: kubeletKeepAlive,
		}).DialContext,
		TLSHandshakeTimeout: kubeletTLSHandshakeTimeout,
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     kubeletIdleConnTimeout,
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
		port = DefaultKubeletPort
	}

	return &kubeletStatsClient{
		httpClient:  &http.Client{Transport: roundTripper, Timeout: cfg.Timeout},
		scheme:      "https",
		defaultPort: port,
	}, nil
}

// GetSummary fetches the stats summary from the kubelet listening at address:port.
func (c *kubeletStatsClient) GetSummary(ctx context.Context, address string, port int32) (*KubeletStats, error) {
	if port == 0 {
		port = c.defaultPort
	}

	url := fmt.Sprintf("%s://%s%s", c.scheme, net.JoinHostPort(address, strconv.Itoa(int(port))), kubeletSummaryPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create kubelet stats request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	started := time.Now()
	resp, err := c.httpClient.Do(req)
	kubeletStatsRequestDuration.Observe(time.Since(started).Seconds())
	if err != nil {
		kubeletStatsRequestsTotal.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("request kubelet stats: %w", err)
	}
	defer func() {
		// Drain before closing so the connection returns to the pool.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, kubeletMaxErrorBodyBytes))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		kubeletStatsRequestsTotal.WithLabelValues("error").Inc()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, kubeletMaxErrorBodyBytes))
		return nil, fmt.Errorf("kubelet stats returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var stats KubeletStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		kubeletStatsRequestsTotal.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("decode kubelet stats: %w", err)
	}

	kubeletStatsRequestsTotal.WithLabelValues("success").Inc()
	return &stats, nil
}

// kubeletAddressForNode resolves where a node's kubelet listens, preferring the internal IP
// the same way Prometheus' node service discovery does. A zero port means the node did not
// advertise its kubelet endpoint and the client default applies.
func kubeletAddressForNode(node *corev1.Node) (string, int32, error) {
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
