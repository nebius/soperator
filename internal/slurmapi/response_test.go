package slurmapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clientWithResponse(t *testing.T, path string, status int, body string) Client {
	t.Helper()
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Equal(t, path, req.URL.Path)
		assert.Equal(t, "test-token", req.Header.Get(headerSlurmUserToken))
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	c, err := NewClient("http://slurmrestd", staticTokenIssuer("test-token"), httpClient)
	require.NoError(t, err)
	return c
}

func TestClient_ListJobs_PriorityAndCPUFrequency(t *testing.T) {
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
		{"per partition priority", `"priority_by_partition":[{"partition":"main","priority":4294967293}]`},
		{"low CPU frequency", `"cpu_frequency_minimum":{"set":true,"number":2147483649}`},
		{"high CPU frequency", `"cpu_frequency_maximum":{"set":true,"number":2147483651}`},
		{"performance governor", `"cpu_frequency_governor":{"set":true,"number":2181038080}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := tt.metadata
			if metadata != "" {
				metadata = "," + metadata
			}
			body := fmt.Sprintf(`{"jobs":[{
				"job_id":361,"job_state":["RUNNING"],"name":"opsd-4-sequential",
				"user_id":1003,"user_name":"adnan","partition":"main","nodes":"worker-1",
				"cpus":{"set":true,"number":48},"memory_per_node":{"set":true,"number":1048576},
				"start_time":{"set":true,"number":1789604574}%s
			},{"job_id":362,"job_state":["PENDING"],"name":"another-job"}],"errors":[]}`, metadata)
			c := clientWithResponse(t, "/slurm/v0.0.44/jobs/", http.StatusOK, body)
			jobs, err := c.ListJobs(context.Background())
			require.NoError(t, err)
			require.Len(t, jobs, 2)
			assert.Equal(t, int32(361), jobs[0].ID)
			assert.Equal(t, "opsd-4-sequential", jobs[0].Name)
			assert.Equal(t, "RUNNING", jobs[0].State)
			assert.Equal(t, "worker-1", jobs[0].Nodes)
			require.NotNil(t, jobs[0].UserID)
			assert.Equal(t, int32(1003), *jobs[0].UserID)
			require.NotNil(t, jobs[0].CPUs)
			assert.Equal(t, int32(48), *jobs[0].CPUs)
			require.NotNil(t, jobs[0].MemoryPerNode)
			assert.Equal(t, int64(1048576), *jobs[0].MemoryPerNode)
			require.NotNil(t, jobs[0].StartTime)
			assert.Equal(t, int64(1789604574), jobs[0].StartTime.Unix())
			assert.Equal(t, int32(362), jobs[1].ID)
		})
	}
}

func TestClient_AccountingJobs_PriorityAndCPUFrequency(t *testing.T) {
	tests := []struct {
		name     string
		metadata string
	}{
		{"TOP priority", `"priority":{"set":true,"number":4294967293}`},
		{"ordinary priority", `"priority":{"set":true,"number":100}`},
		{"step CPU frequency", `"steps":[{"CPU":{"requested_frequency":{"min":{"set":true,"number":2147483649},"max":{"set":true,"number":2147483651}},"governor":"Performance"}}]`},
	}
	for _, tt := range tests {
		for _, singleJob := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/singleJob=%t", tt.name, singleJob), func(t *testing.T) {
				body := fmt.Sprintf(`{"jobs":[{
					"job_id":361,"state":{"current":["RUNNING"],"reason":"None"},
					"association":{"id":1,"user":"adnan"},
					"array":{"job_id":350,"task_id":{"set":true,"number":2}},
					"time":{"start":1789604574,"end":0,"submission":1789604563,"limit":{"set":true,"number":1440}},
					"required":{"CPUs":48,"memory_per_node":{"set":true,"number":1048576}},
					"tres":{"allocated":[{"type":"cpu","count":48},{"type":"mem","count":1048576}]},
					%s
				}],"errors":[]}`, tt.metadata)
				path := "/slurmdb/v0.0.44/jobs/"
				if singleJob {
					path = "/slurmdb/v0.0.44/job/361"
				}
				c := clientWithResponse(t, path, http.StatusOK, body)
				var jobs []Job
				var err error
				if singleJob {
					jobs, err = c.GetJobsByIDFromAccounting(context.Background(), "361")
				} else {
					jobs, err = c.ListJobsWithParams(context.Background(), ListJobsParams{Source: JobSourceAccounting, AccountingLookback: time.Hour})
				}
				require.NoError(t, err)
				require.Len(t, jobs, 1)
				assert.Equal(t, int32(361), jobs[0].ID)
				assert.Equal(t, "adnan", jobs[0].UserName)
				assert.Equal(t, "2", jobs[0].GetArrayTaskIDString())
				require.NotNil(t, jobs[0].EndTime)
				assert.Equal(t, int64(1789604574+86400), jobs[0].EndTime.Unix())
				assert.Equal(t, "cpu=48,mem=1048576M", jobs[0].TresAllocated)
			})
		}
	}
}

func TestClient_GetDiag_RPCCountOverflow(t *testing.T) {
	const body = `{"statistics":{
		"server_thread_count":3,"schedule_cycle_sum":1234,
		"rpcs_by_message_type":[{"message_type":"REQUEST_JOB_INFO","count":4294967293,"total_time":5678}],
		"rpcs_by_user":[{"user":"adnan","user_id":1003,"count":4294967295,"total_time":9012}]
	}}`
	c := clientWithResponse(t, "/slurm/v0.0.44/diag/", http.StatusOK, body)
	diag, err := c.GetDiag(context.Background())
	require.NoError(t, err)
	require.NotNil(t, diag.Statistics.RpcsByMessageType)
	rpc := (*diag.Statistics.RpcsByMessageType)[0]
	assert.Equal(t, int64(4294967293), rpc.Count)
	assert.Equal(t, int64(5678), rpc.TotalTime)
	require.NotNil(t, diag.Statistics.RpcsByUser)
	user := (*diag.Statistics.RpcsByUser)[0]
	assert.Equal(t, int32(1003), user.UserId)
	assert.Equal(t, int64(4294967295), user.Count)
	assert.Equal(t, int64(9012), user.TotalTime)
}

func TestClient_ListJobs_ContentType(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`{"jobs":[]}`)),
		}, nil
	})}
	c, err := NewClient("http://slurmrestd", nil, httpClient)
	require.NoError(t, err)
	_, err = c.ListJobs(context.Background())
	require.ErrorContains(t, err, "decode Slurm response: unexpected content type")
}

func TestClient_ListJobs_ResponseErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"HTTP error", 503, `{"errors":[{"description":"controller unavailable"}]}`, "status=503 errors=[controller unavailable]"},
		{"Slurm error", 200, `{"errors":[{"description":"access denied"}]}`, "errors=[access denied]"},
		{"invalid JSON", 200, `{"jobs":[`, "decode Slurm response"},
		{"invalid consumed field", 200, `{"jobs":[{"job_id":361,"job_state":["RUNNING"],"cpus":{"set":true,"number":"bad"}}]}`, "decode Slurm response"},
		{"missing ID", 200, `{"jobs":[{"job_state":["RUNNING"]}]}`, "job ID is missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := clientWithResponse(t, "/slurm/v0.0.44/jobs/", tt.status, tt.body)
			_, err := c.ListJobs(context.Background())
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestClient_ListJobs_ServerPrefix(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "/rest/slurm/v0.0.44/jobs/", req.URL.Path)
		assert.Equal(t, "test-token", req.Header.Get(headerSlurmUserToken))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"jobs":[]}`)),
		}, nil
	})}
	c, err := NewClient("http://slurmrestd/rest/", staticTokenIssuer("test-token"), httpClient)
	require.NoError(t, err)
	jobs, err := c.ListJobs(context.Background())
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

type trackedResponseBody struct {
	io.Reader
	closed bool
}

func (b *trackedResponseBody) Close() error {
	b.closed = true
	return nil
}

func TestDecodeJobsResponse_ClosesBody(t *testing.T) {
	for _, body := range []string{`{"jobs":[]}`, `{"jobs":[`} {
		t.Run(body, func(t *testing.T) {
			responseBody := &trackedResponseBody{Reader: strings.NewReader(body)}
			_, _ = decodeJobsResponse[controllerJob](&http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       responseBody,
			})
			assert.True(t, responseBody.closed)
		})
	}
}
