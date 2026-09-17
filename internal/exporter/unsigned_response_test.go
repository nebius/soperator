package exporter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

func TestMetricsCollector_UnsignedSlurmResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/slurm/v0.0.44/nodes/":
			_, _ = w.Write([]byte(`{"nodes":[]}`))
		case "/slurm/v0.0.44/jobs/":
			_, _ = w.Write([]byte(`{"jobs":[{"job_id":361,"job_state":["RUNNING"],"user_id":4294967293,"user_name":"adnan","cpus":{"set":true,"number":48},"priority":{"set":true,"number":4294967292}}]}`))
		case "/slurm/v0.0.44/diag/":
			_, _ = w.Write([]byte(`{"statistics":{"rpcs_by_message_type":[{"message_type":"REQUEST_JOB_INFO","count":4294967293,"total_time":6871947674}],"rpcs_by_user":[{"user":"adnan","user_id":4294967293,"count":4294967292,"total_time":7384185912}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := slurmapi.NewClient(server.URL, nil, server.Client())
	require.NoError(t, err)
	collector := newTestMetricsCollector(client)
	require.NoError(t, collectOnce(context.Background(), collector))
	registry := prometheus.NewRegistry()
	require.NoError(t, registry.Register(collector))
	families, err := registry.Gather()
	require.NoError(t, err)

	var found []string
	for _, family := range families {
		switch family.GetName() {
		case "slurm_job_cpus":
			require.Len(t, family.Metric, 1)
			assert.Equal(t, float64(48), family.Metric[0].GetGauge().GetValue())
		case "slurm_job_info":
			require.Len(t, family.Metric, 1)
			labels := make(map[string]string)
			for _, label := range family.Metric[0].Label {
				labels[label.GetName()] = label.GetValue()
			}
			assert.Equal(t, "361", labels["job_id"])
			assert.Equal(t, "4294967293", labels["user_id"])
		case "slurm_controller_rpc_calls_total":
			require.Len(t, family.Metric, 1)
			assert.Equal(t, float64(4294967293), family.Metric[0].GetCounter().GetValue())
		case "slurm_controller_rpc_user_calls_total":
			require.Len(t, family.Metric, 1)
			assert.Equal(t, float64(4294967292), family.Metric[0].GetCounter().GetValue())
		default:
			continue
		}
		found = append(found, family.GetName())
	}
	assert.ElementsMatch(t, []string{"slurm_job_cpus", "slurm_job_info", "slurm_controller_rpc_calls_total", "slurm_controller_rpc_user_calls_total"}, found)
}
