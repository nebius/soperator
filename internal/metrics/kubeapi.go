package metrics

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"nebius.ai/slurm-operator/internal/controllerconfig"
)

// Value of the controller label for requests that are not issued from a reconcile:
// informer list/watch, leader election, webhooks, manager runnables.
const kubeAPIControllerManager = "manager"

var kubeAPIRequestInfo = request.RequestInfoFactory{
	APIPrefixes:          sets.NewString("api", "apis"),
	GrouplessAPIPrefixes: sets.NewString("api"),
}

type kubeAPIMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

var defaultKubeAPIMetrics = newKubeAPIMetrics(metrics.Registry)

func newKubeAPIMetrics(reg prometheus.Registerer) *kubeAPIMetrics {
	m := &kubeAPIMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "soperator",
			Subsystem: "kube_api",
			Name:      "requests_total",
			Help: "Requests sent to the Kubernetes API server, attributed to the controller whose reconcile " +
				"issued them; controller=\"manager\" covers informers, leader election, webhooks and runnables. " +
				"Cache reads never reach the API server and are not counted.",
		}, []string{"controller", "verb", "resource", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "soperator",
			Subsystem: "kube_api",
			Name:      "request_duration_seconds",
			Help: "Kubernetes API server request latency from sending the request to receiving the response " +
				"headers; client-side rate limiter wait is not included.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"controller", "verb", "resource"}),
	}
	reg.MustRegister(m.requests, m.duration)
	return m
}

// InstrumentRESTConfig wraps the transport of cfg so that every request to the API server is
// recorded in the soperator_kube_api_* metrics. Requests that the same config sends elsewhere,
// for example straight to a kubelet, pass through unrecorded. Call it before the config is handed
// to the manager or to any other client.
func InstrumentRESTConfig(cfg *rest.Config) {
	host := apiServerHost(cfg.Host)
	cfg.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return &kubeAPITransport{next: next, host: host, metrics: defaultKubeAPIMetrics}
	})
}

func apiServerHost(configHost string) string {
	if !strings.Contains(configHost, "://") {
		configHost = "https://" + configHost
	}
	u, err := url.Parse(configHost)
	if err != nil {
		return configHost
	}
	return u.Host
}

type kubeAPITransport struct {
	next    http.RoundTripper
	host    string
	metrics *kubeAPIMetrics
}

func (t *kubeAPITransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.EqualFold(req.URL.Host, t.host) {
		return t.next.RoundTrip(req)
	}

	controller := controllerconfig.ControllerNameFromContext(req.Context())
	if controller == "" {
		controller = kubeAPIControllerManager
	}
	verb, resource := classifyKubeAPIRequest(req)

	start := time.Now()
	resp, err := t.next.RoundTrip(req)
	code := "error"
	if err == nil {
		code = strconv.Itoa(resp.StatusCode)
	}
	t.metrics.requests.WithLabelValues(controller, verb, resource, code).Inc()
	t.metrics.duration.WithLabelValues(controller, verb, resource).Observe(time.Since(start).Seconds())
	return resp, err
}

// classifyKubeAPIRequest maps a request to the Kubernetes verb and a bounded resource label such as
// "pods", "pods/status" or "apps.kruise.io/statefulsets". Parsing is delegated to the same
// RequestInfoFactory the API server uses; object names and namespaces never reach the label.
// Non-resource paths (/healthz, /version, /openapi) report their first segment, API discovery
// reports "discovery".
func classifyKubeAPIRequest(req *http.Request) (verb, resource string) {
	info, err := kubeAPIRequestInfo.NewRequestInfo(req)
	if err != nil || !info.IsResourceRequest || info.Resource == "" {
		first, _, _ := strings.Cut(strings.Trim(req.URL.Path, "/"), "/")
		if first == "api" || first == "apis" {
			return "get", "discovery"
		}
		return strings.ToLower(req.Method), first
	}

	resource = info.Resource
	if info.APIGroup != "" {
		resource = info.APIGroup + "/" + resource
	}
	if info.Subresource != "" {
		resource += "/" + info.Subresource
	}
	return info.Verb, resource
}
