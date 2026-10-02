package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"nebius.ai/slurm-operator/internal/controllerconfig"
)

// Value of the controller label for Slurm REST requests issued outside a tagged context.
const slurmRESTControllerManager = "manager"

// Path segments that follow a resource name without being an object id, e.g. /nodes/reboot or /job/submit.
var slurmRESTActions = map[string]bool{"reboot": true, "submit": true, "allocate": true}

type slurmRESTMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	retries  *prometheus.CounterVec
}

var defaultSlurmRESTMetrics = newSlurmRESTMetrics(metrics.Registry)

func newSlurmRESTMetrics(reg prometheus.Registerer) *slurmRESTMetrics {
	m := &slurmRESTMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "soperator",
			Subsystem: "slurm_rest",
			Name:      "requests_total",
			Help: "HTTP attempts sent to slurmrestd, one per attempt including retries, attributed to the controller " +
				"whose reconcile issued them; nodecache and exporter are background loops, manager is anything untagged. " +
				"host is the per-cluster REST service.",
		}, []string{"controller", "host", "method", "path", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "soperator",
			Subsystem: "slurm_rest",
			Name:      "request_duration_seconds",
			Help:      "slurmrestd request latency per attempt, from sending the request to receiving the response headers.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		}, []string{"controller", "method", "path"}),
		retries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "soperator",
			Subsystem: "slurm_rest",
			Name:      "retries_total",
			Help:      "Retried slurmrestd attempts after a failed or retryable response.",
		}, []string{"controller", "host", "path"}),
	}
	reg.MustRegister(m.requests, m.duration, m.retries)
	return m
}

// SlurmRESTCollectors returns the Slurm REST client metrics so that a process without a
// controller-runtime manager (the exporter) can expose them from its own registry.
func SlurmRESTCollectors() []prometheus.Collector {
	m := defaultSlurmRESTMetrics
	return []prometheus.Collector{m.requests, m.duration, m.retries}
}

// InstrumentRetryableClient records every attempt the retryable client makes in the
// soperator_slurm_rest_* metrics. It wraps the inner transport rather than the retrying one,
// because the client's ErrorHandler hides the final status once retries are exhausted.
func InstrumentRetryableClient(c *retryablehttp.Client) {
	defaultSlurmRESTMetrics.instrument(c)
}

func (m *slurmRESTMetrics) instrument(c *retryablehttp.Client) {
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{}
	}
	next := c.HTTPClient.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	c.HTTPClient.Transport = &slurmRESTTransport{next: next, metrics: m}

	previousHook := c.RequestLogHook
	c.RequestLogHook = func(logger retryablehttp.Logger, req *http.Request, attempt int) {
		if attempt > 0 {
			m.retries.WithLabelValues(slurmRESTController(req), req.URL.Host, normalizeSlurmRESTPath(req.URL.Path)).Inc()
		}
		if previousHook != nil {
			previousHook(logger, req, attempt)
		}
	}
}

type slurmRESTTransport struct {
	next    http.RoundTripper
	metrics *slurmRESTMetrics
}

func (t *slurmRESTTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	controller := slurmRESTController(req)
	path := normalizeSlurmRESTPath(req.URL.Path)

	start := time.Now()
	resp, err := t.next.RoundTrip(req)
	code := "error"
	if err == nil {
		code = strconv.Itoa(resp.StatusCode)
	}
	t.metrics.requests.WithLabelValues(controller, req.URL.Host, req.Method, path, code).Inc()
	t.metrics.duration.WithLabelValues(controller, req.Method, path).Observe(time.Since(start).Seconds())
	return resp, err
}

func slurmRESTController(req *http.Request) string {
	if name := controllerconfig.ControllerNameFromContext(req.Context()); name != "" {
		return name
	}
	return slurmRESTControllerManager
}

// normalizeSlurmRESTPath turns a slurmrestd path into a bounded path label. The label is not called
// endpoint because service discovery already sets endpoint to the scrape port name:
// /slurm/v0.0.44/node/worker-12 becomes /slurm/v0.0.44/node/{id}, /slurm/v0.0.44/nodes/ becomes
// /slurm/v0.0.44/nodes. The API is /{slurm|slurmdb}/{version}/{resource}[/{id}|/{action}], so only
// the fourth segment can carry an object name; known actions stay literal.
func normalizeSlurmRESTPath(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 3 {
		return "/" + strings.Join(segments, "/")
	}
	normalized := segments[:3]
	if len(segments) > 3 {
		fourth := segments[3]
		if !slurmRESTActions[fourth] {
			fourth = "{id}"
		}
		normalized = append(normalized, fourth)
	}
	return "/" + strings.Join(normalized, "/")
}
