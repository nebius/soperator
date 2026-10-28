package slurmapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type jobOverflowTokenIssuer struct{}

func (jobOverflowTokenIssuer) Issue(context.Context) (string, error) {
	return "test-token", nil
}

func jobOverflowClient(t *testing.T, singleJob bool, status int, body string) Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := "/slurm/v0.0.41/jobs/"
		if singleJob {
			path = "/slurm/v0.0.41/job/361"
		}
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, path, r.URL.Path)
		assert.Equal(t, "test-token", r.Header.Get(headerSlurmUserToken))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	c, err := NewClient(server.URL, jobOverflowTokenIssuer{}, server.Client())
	require.NoError(t, err)
	return c
}

func jobOverflowFetch(ctx context.Context, c Client, singleJob bool) ([]Job, error) {
	if singleJob {
		return c.GetJobsByID(ctx, "361")
	}
	return c.ListJobs(ctx)
}

func TestClient_Jobs_PriorityAndCPUFrequency(t *testing.T) {
	tests := []struct {
		name     string
		metadata string
	}{
		{"TOP priority", `"priority":{"set":true,"number":4294967293}`},
		{"next priority", `"priority":{"set":true,"number":4294967292}`},
		{"priority above signed boundary", `"priority":{"set":true,"number":2147483648}`},
		{"ordinary priority", `"priority":{"set":true,"number":100}`},
		{"unset priority", `"priority":{"set":false,"number":0}`},
		{"null priority", `"priority":null`},
		{"omitted metadata", ""},
		{"low CPU frequency", `"cpu_frequency_minimum":{"set":true,"number":2147483649}`},
		{"high CPU frequency", `"cpu_frequency_maximum":{"set":true,"number":2147483651}`},
		{"performance governor", `"cpu_frequency_governor":{"set":true,"number":2181038080}`},
		{"unset CPU frequency", `"cpu_frequency_minimum":{"set":false,"number":0},"cpu_frequency_maximum":null,"cpu_frequency_governor":null`},
	}
	for _, tt := range tests {
		for _, singleJob := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/singleJob=%t", tt.name, singleJob), func(t *testing.T) {
				metadata := tt.metadata
				if metadata != "" {
					metadata = "," + metadata
				}
				body := fmt.Sprintf(`{"jobs":[{
					"job_id":361,"job_state":["RUNNING"],"name":"opsd-4-sequential",
					"user_id":1003,"user_name":"adnan","partition":"main","nodes":"worker-1",
					"node_count":{"set":true,"number":1},"array_job_id":{"set":true,"number":350},"array_task_id":{"set":true,"number":2},
					"cpus":{"set":true,"number":48},"memory_per_node":{"set":true,"number":1048576},
					"start_time":{"set":true,"number":1789604574}%s
				}],"errors":[]}`, metadata)
				c := jobOverflowClient(t, singleJob, http.StatusOK, body)
				jobs, err := jobOverflowFetch(context.Background(), c, singleJob)
				require.NoError(t, err)
				require.Len(t, jobs, 1)
				job := jobs[0]
				assert.Equal(t, int32(361), job.ID)
				assert.Equal(t, "opsd-4-sequential", job.Name)
				assert.Equal(t, "RUNNING", job.State)
				assert.Equal(t, "worker-1", job.Nodes)
				assert.Equal(t, "main", job.Partition)
				assert.Equal(t, "adnan", job.UserName)
				assert.Equal(t, "350", job.GetArrayJobIDString())
				assert.Equal(t, "2", job.GetArrayTaskIDString())
				require.NotNil(t, job.UserID)
				assert.Equal(t, int32(1003), *job.UserID)
				require.NotNil(t, job.NodeCount)
				assert.Equal(t, int32(1), *job.NodeCount)
				require.NotNil(t, job.CPUs)
				assert.Equal(t, int32(48), *job.CPUs)
				require.NotNil(t, job.MemoryPerNode)
				assert.Equal(t, int64(1048576), *job.MemoryPerNode)
				require.NotNil(t, job.StartTime)
				assert.Equal(t, int64(1789604574), job.StartTime.Unix())
			})
		}
	}
}

func TestClient_Jobs_ResponseErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"HTTP error", 503, `{"errors":[{"description":"controller unavailable"}]}`, "status=503"},
		{"Slurm error", 200, `{"errors":[{"description":"access denied"}]}`, "access denied"},
		{"invalid JSON", 200, `{"jobs":[`, "decode Slurm response"},
		{"invalid consumed field", 200, `{"jobs":[{"job_id":361,"job_state":["RUNNING"],"cpus":{"set":true,"number":"bad"}}]}`, "decode Slurm response"},
		{"missing ID", 200, `{"jobs":[{"job_state":["RUNNING"]}]}`, "job ID is missing"},
	}
	for _, tt := range tests {
		for _, singleJob := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/singleJob=%t", tt.name, singleJob), func(t *testing.T) {
				c := jobOverflowClient(t, singleJob, tt.status, tt.body)
				_, err := jobOverflowFetch(context.Background(), c, singleJob)
				require.ErrorContains(t, err, tt.want)
			})
		}
	}
}
