package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"nebius.ai/slurm-operator/internal/controllerconfig"
)

func TestNormalizeSlurmRESTPath(t *testing.T) {
	tests := map[string]string{
		"/slurm/v0.0.44/nodes/":         "/slurm/v0.0.44/nodes",
		"/slurm/v0.0.44/node/worker-12": "/slurm/v0.0.44/node/{id}",
		"/slurm/v0.0.44/nodes/reboot":   "/slurm/v0.0.44/nodes/reboot",
		"/slurm/v0.0.44/jobs/":          "/slurm/v0.0.44/jobs",
		"/slurm/v0.0.44/job/submit":     "/slurm/v0.0.44/job/submit",
		"/slurm/v0.0.44/job/12345":      "/slurm/v0.0.44/job/{id}",
		"/slurm/v0.0.44/diag/":          "/slurm/v0.0.44/diag",
		"/slurm/v0.0.44/reconfigure/":   "/slurm/v0.0.44/reconfigure",
		"/slurmdb/v0.0.44/job/12345":    "/slurmdb/v0.0.44/job/{id}",
		"/slurmdb/v0.0.44/jobs/":        "/slurmdb/v0.0.44/jobs",
		"/slurm/v0.0.44/node/a/b/c":     "/slurm/v0.0.44/node/{id}",
		"/openapi/v3":                   "/openapi/v3",
		"/":                             "/",
	}
	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			require.Equal(t, want, normalizeSlurmRESTPath(path))
		})
	}
}

func newInstrumentedRetryClient(t *testing.T, reg prometheus.Registerer) *http.Client {
	t.Helper()
	m := newSlurmRESTMetrics(reg)
	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil
	retryClient.RetryMax = 3
	retryClient.RetryWaitMin = time.Millisecond
	retryClient.RetryWaitMax = time.Millisecond
	m.instrument(retryClient)
	return retryClient.StandardClient()
}

func TestSlurmRESTTransportRecordsEveryAttempt(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reg := prometheus.NewRegistry()
	client := newInstrumentedRetryClient(t, reg)
	ctx := controllerconfig.WithControllerName(context.Background(), "rollingupdate")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/slurm/v0.0.44/node/worker-3", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	host := req.URL.Host
	require.Equal(t, map[string]float64{
		"controller=rollingupdate,host=" + host + ",method=POST,path=/slurm/v0.0.44/node/{id},code=503": 1,
		"controller=rollingupdate,host=" + host + ",method=POST,path=/slurm/v0.0.44/node/{id},code=200": 1,
	}, gatherSlurmCounters(t, reg, "soperator_slurm_rest_requests_total"))
	require.Equal(t, map[string]float64{
		"controller=rollingupdate,host=" + host + ",path=/slurm/v0.0.44/node/{id}": 1,
	}, gatherSlurmCounters(t, reg, "soperator_slurm_rest_retries_total"))
	require.Equal(t, map[string]uint64{
		"controller=rollingupdate,method=POST,path=/slurm/v0.0.44/node/{id}": 2,
	}, gatherSlurmHistogramCounts(t, reg, "soperator_slurm_rest_request_duration_seconds"))
}

func TestSlurmRESTTransportRecordsExhaustedRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	reg := prometheus.NewRegistry()
	client := newInstrumentedRetryClient(t, reg)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/slurm/v0.0.44/nodes/", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.Error(t, err)
	require.Nil(t, resp)

	counters := gatherSlurmCounters(t, reg, "soperator_slurm_rest_requests_total")
	require.Equal(t, map[string]float64{
		"controller=manager,host=" + req.URL.Host + ",method=GET,path=/slurm/v0.0.44/nodes,code=503": 4,
	}, counters)
	require.Equal(t, map[string]float64{
		"controller=manager,host=" + req.URL.Host + ",path=/slurm/v0.0.44/nodes": 3,
	}, gatherSlurmCounters(t, reg, "soperator_slurm_rest_retries_total"))
}

func gatherSlurmCounters(t *testing.T, g prometheus.Gatherer, name string) map[string]float64 {
	t.Helper()
	out := make(map[string]float64)
	for _, m := range gatherMetric(t, g, name) {
		out[slurmLabelKey(m)] = m.GetCounter().GetValue()
	}
	return out
}

func gatherSlurmHistogramCounts(t *testing.T, g prometheus.Gatherer, name string) map[string]uint64 {
	t.Helper()
	out := make(map[string]uint64)
	for _, m := range gatherMetric(t, g, name) {
		out[slurmLabelKey(m)] = m.GetHistogram().GetSampleCount()
	}
	return out
}

func slurmLabelKey(m *dto.Metric) string {
	values := make(map[string]string, len(m.GetLabel()))
	for _, lp := range m.GetLabel() {
		values[lp.GetName()] = lp.GetValue()
	}
	var parts []string
	for _, name := range []string{"controller", "host", "method", "path", "code"} {
		if value, ok := values[name]; ok {
			parts = append(parts, name+"="+value)
		}
	}
	return strings.Join(parts, ",")
}
