package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"nebius.ai/slurm-operator/internal/controllerconfig"
)

func TestClassifyKubeAPIRequest(t *testing.T) {
	tests := []struct {
		method, rawURL, verb, resource string
	}{
		{http.MethodGet, "/api/v1/namespaces/soperator/pods/worker-0", "get", "pods"},
		{http.MethodGet, "/api/v1/namespaces/soperator/pods", "list", "pods"},
		{http.MethodGet, "/api/v1/namespaces/soperator/pods?watch=true&resourceVersion=1", "watch", "pods"},
		{http.MethodGet, "/api/v1/pods?watch=1", "watch", "pods"},
		{http.MethodGet, "/api/v1/nodes/node-a", "get", "nodes"},
		{http.MethodGet, "/api/v1/nodes/node-a/proxy/pods", "get", "nodes/proxy"},
		{http.MethodGet, "/api/v1/namespaces/soperator", "get", "namespaces"},
		{http.MethodGet, "/api/v1/namespaces", "list", "namespaces"},
		{http.MethodPut, "/api/v1/namespaces/soperator/finalize", "update", "namespaces/finalize"},
		{http.MethodPatch, "/api/v1/namespaces/soperator/pods/worker-0/status", "patch", "pods/status"},
		{http.MethodPost, "/apis/apps/v1/namespaces/soperator/deployments", "create", "apps/deployments"},
		{http.MethodPut, "/apis/apps.kruise.io/v1beta1/namespaces/soperator/statefulsets/worker/status", "update", "apps.kruise.io/statefulsets/status"},
		{http.MethodPatch, "/apis/slurm.nebius.ai/v1alpha1/namespaces/soperator/nodesets/worker", "patch", "slurm.nebius.ai/nodesets"},
		{http.MethodDelete, "/apis/batch/v1/namespaces/soperator/jobs/check-1", "delete", "batch/jobs"},
		{http.MethodDelete, "/apis/batch/v1/namespaces/soperator/jobs", "deletecollection", "batch/jobs"},
		{http.MethodPut, "/apis/coordination.k8s.io/v1/namespaces/soperator-system/leases/lock", "update", "coordination.k8s.io/leases"},
		{http.MethodGet, "/api", "get", "discovery"},
		{http.MethodGet, "/apis", "get", "discovery"},
		{http.MethodGet, "/api/v1", "get", "discovery"},
		{http.MethodGet, "/apis/apps/v1", "get", "discovery"},
		{http.MethodGet, "/healthz", "get", "healthz"},
		{http.MethodGet, "/version", "get", "version"},
		{http.MethodGet, "/openapi/v3", "get", "openapi"},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.rawURL, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), tc.method, "https://apiserver"+tc.rawURL, nil)
			require.NoError(t, err)
			verb, resource := classifyKubeAPIRequest(req)
			require.Equal(t, tc.verb, verb)
			require.Equal(t, tc.resource, resource)
		})
	}
}

func TestKubeAPITransportRecordsAPIServerRequestsOnly(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer apiServer.Close()
	kubelet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer kubelet.Close()

	reg := prometheus.NewRegistry()
	m := newKubeAPIMetrics(reg)
	cfg := &rest.Config{Host: apiServer.URL}
	cfg.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return &kubeAPITransport{next: next, host: apiServerHost(cfg.Host), metrics: m}
	})
	client, err := rest.HTTPClientFor(cfg)
	require.NoError(t, err)

	do := func(ctx context.Context, method, rawURL string) {
		req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	tagged := controllerconfig.WithControllerName(context.Background(), "nodeset")
	do(tagged, http.MethodPatch, apiServer.URL+"/apis/slurm.nebius.ai/v1alpha1/namespaces/soperator/nodesets/worker/status")
	do(tagged, http.MethodPatch, apiServer.URL+"/apis/slurm.nebius.ai/v1alpha1/namespaces/soperator/nodesets/worker/status")
	do(context.Background(), http.MethodGet, apiServer.URL+"/api/v1/pods?watch=true")
	do(tagged, http.MethodGet, kubelet.URL+"/pods")

	counters := gatherCounters(t, reg, "soperator_kube_api_requests_total")
	require.Equal(t, map[string]float64{
		"controller=nodeset,verb=patch,resource=slurm.nebius.ai/nodesets/status,code=409": 2,
		"controller=manager,verb=watch,resource=pods,code=409":                            1,
	}, counters)

	histograms := gatherHistogramCounts(t, reg, "soperator_kube_api_request_duration_seconds")
	require.Equal(t, map[string]uint64{
		"controller=nodeset,verb=patch,resource=slurm.nebius.ai/nodesets/status": 2,
		"controller=manager,verb=watch,resource=pods":                            1,
	}, histograms)
}

func TestAPIServerHost(t *testing.T) {
	require.Equal(t, "10.0.0.1:443", apiServerHost("https://10.0.0.1:443"))
	require.Equal(t, "kubernetes.default.svc", apiServerHost("kubernetes.default.svc"))
	require.Equal(t, "api.example.com:6443", apiServerHost("https://api.example.com:6443/"))
}

func gatherCounters(t *testing.T, g prometheus.Gatherer, name string) map[string]float64 {
	t.Helper()
	out := make(map[string]float64)
	for _, m := range gatherMetric(t, g, name) {
		out[labelKey(m)] = m.GetCounter().GetValue()
	}
	return out
}

func gatherHistogramCounts(t *testing.T, g prometheus.Gatherer, name string) map[string]uint64 {
	t.Helper()
	out := make(map[string]uint64)
	for _, m := range gatherMetric(t, g, name) {
		out[labelKey(m)] = m.GetHistogram().GetSampleCount()
	}
	return out
}

func gatherMetric(t *testing.T, g prometheus.Gatherer, name string) []*dto.Metric {
	t.Helper()
	families, err := g.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == name {
			return family.GetMetric()
		}
	}
	t.Fatalf("metric %s not gathered", name)
	return nil
}

// labelKey renders labels in reading order rather than the alphabetical order prometheus gathers them in.
func labelKey(m *dto.Metric) string {
	values := make(map[string]string, len(m.GetLabel()))
	for _, lp := range m.GetLabel() {
		values[lp.GetName()] = lp.GetValue()
	}
	var parts []string
	for _, name := range []string{"controller", "verb", "resource", "code"} {
		if value, ok := values[name]; ok {
			parts = append(parts, name+"="+value)
		}
	}
	return strings.Join(parts, ",")
}
