package slurmapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
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

func TestClient_ListJobs_UnsignedMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata string
	}{
		{"priority from job 351_2", `"priority":{"set":true,"number":4294967293}`},
		{"priority from job 361", `"priority":{"set":true,"number":4294967292}`},
		{"priority above signed boundary", `"priority":{"set":true,"number":2147483648}`},
		{"maximum priority", `"priority":{"set":true,"number":4294967295}`},
		{"per partition priority", `"priority_by_partition":[{"partition":"main","priority":4294967293}]`},
		{"nice", `"nice":4294967293`},
		{"CPU frequency", `"cpu_frequency_minimum":{"set":true,"number":2147483649},"cpu_frequency_maximum":{"set":true,"number":2147483650},"cpu_frequency_governor":{"set":true,"number":2147483651}`},
		{"group ID", `"group_id":4294967293`},
		{"unused uint64", `"accrue_time":{"set":false,"number":18446744073709551614}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"jobs":[{
				"job_id":361,"job_state":["RUNNING"],"name":"opsd-4-sequential",
				"user_id":1003,"user_name":"adnan","partition":"main","nodes":"worker-1",
				"cpus":{"set":true,"number":48},"memory_per_node":{"set":true,"number":1048576},
				"start_time":{"set":true,"number":1789604574},
				%s
			},{"job_id":362,"job_state":["PENDING"],"name":"another-job"}],"errors":[]}`, tt.metadata)
			c := clientWithResponse(t, "/slurm/v0.0.44/jobs/", http.StatusOK, body)
			jobs, err := c.ListJobs(context.Background())
			require.NoError(t, err)
			require.Len(t, jobs, 2)
			assert.Equal(t, int32(361), jobs[0].ID)
			assert.Equal(t, "opsd-4-sequential", jobs[0].Name)
			assert.Equal(t, "RUNNING", jobs[0].State)
			assert.Equal(t, "worker-1", jobs[0].Nodes)
			require.NotNil(t, jobs[0].UserID)
			assert.Equal(t, int64(1003), *jobs[0].UserID)
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

func TestClient_AccountingJobs_UnsignedMetadata(t *testing.T) {
	const body = `{"jobs":[{
		"job_id":361,"state":{"current":["RUNNING"],"reason":"None"},
		"priority":{"set":true,"number":4294967292},
		"association":{"id":4294967293,"user":"adnan"},
		"array":{"job_id":350,"task_id":{"set":true,"number":2},"limits":{"max":{"running":{"tasks":4294967293}}}},
		"time":{"start":1789604574,"end":0,"submission":1789604563,
			"limit":{"set":true,"number":1440},"elapsed":4294967293,
			"planned":{"set":false,"number":18446744073709551614}},
		"required":{"CPUs":48,"memory_per_cpu":{"set":true,"number":18446744073709551615},"memory_per_node":{"set":true,"number":1048576}},
		"tres":{"allocated":[{"type":"cpu","count":48},{"type":"mem","count":1048576}]},
		"steps":[{"statistics":{"CPU":{"actual_frequency":18446744073709551615}}}]
	}],"errors":[]}`
	for _, singleJob := range []bool{false, true} {
		t.Run(fmt.Sprintf("singleJob=%t", singleJob), func(t *testing.T) {
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

func TestClient_ListJobs_NumberFlags(t *testing.T) {
	const body = `{"jobs":[{"job_id":361,"job_state":["RUNNING"],
		"array_job_id":{"set":false,"number":4294967294},
		"array_task_id":{"set":false,"number":4294967294},
		"node_count":{"set":true,"infinite":true,"number":4294967295},
		"memory_per_node":{"set":true,"infinite":true,"number":18446744073709551615},
		"end_time":{"set":false,"number":18446744073709551614}
	}]}`
	c := clientWithResponse(t, "/slurm/v0.0.44/jobs/", http.StatusOK, body)
	jobs, err := c.ListJobs(context.Background())
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Nil(t, jobs[0].ArrayJobID)
	assert.Nil(t, jobs[0].ArrayTaskID)
	assert.Nil(t, jobs[0].NodeCount)
	assert.Nil(t, jobs[0].MemoryPerNode)
	assert.Nil(t, jobs[0].EndTime)
}

func TestClient_GetDiag_UnsignedCounters(t *testing.T) {
	const body = `{"statistics":{
		"server_thread_count":3,"jobs_started":4294967293,"bf_depth_sum":4294967295,
		"schedule_cycle_sum":18446744073709551615,
		"rpcs_by_message_type":[{"message_type":"REQUEST_JOB_INFO","count":4294967293,"total_time":18446744073709551615}],
		"rpcs_by_user":[{"user":"adnan","user_id":4294967293,"count":4294967295,"total_time":9223372036854775808}]
	}}`
	c := clientWithResponse(t, "/slurm/v0.0.44/diag/", http.StatusOK, body)
	diag, err := c.GetDiag(context.Background())
	require.NoError(t, err)
	require.NotNil(t, diag.Statistics.RpcsByMessageType)
	rpc := (*diag.Statistics.RpcsByMessageType)[0]
	assert.Equal(t, uint64(4294967293), rpc.Count)
	assert.Equal(t, uint64(math.MaxUint64), rpc.TotalTime)
	require.NotNil(t, diag.Statistics.RpcsByUser)
	user := (*diag.Statistics.RpcsByUser)[0]
	assert.Equal(t, uint32(4294967293), user.UserId)
	assert.Equal(t, uint64(4294967295), user.Count)
	assert.Equal(t, uint64(9223372036854775808), user.TotalTime)
}

func TestClient_Nodes_UnsignedMetadata(t *testing.T) {
	fixture, err := os.ReadFile("testdata/usual_node_rest.json")
	require.NoError(t, err)
	// Replace the fixture's weight without decoding its numbers through float64.
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(fixture, &fields))
	fields["weight"] = json.RawMessage(`4294967293`)
	fields["free_mem"] = json.RawMessage(`{"set":true,"infinite":true,"number":18446744073709551615}`)
	node, err := json.Marshal(fields)
	require.NoError(t, err)
	body := `{"nodes":[` + string(node) + `]}`
	for _, singleNode := range []bool{false, true} {
		t.Run(fmt.Sprintf("singleNode=%t", singleNode), func(t *testing.T) {
			path := "/slurm/v0.0.44/nodes/"
			if singleNode {
				path = "/slurm/v0.0.44/node/worker-1"
			}
			c := clientWithResponse(t, path, http.StatusOK, body)
			var n Node
			if singleNode {
				n, err = c.GetNode(context.Background(), "worker-1")
			} else {
				var nodes []Node
				nodes, err = c.ListNodes(context.Background())
				require.NoError(t, err)
				require.Len(t, nodes, 1)
				n = nodes[0]
			}
			require.NoError(t, err)
			assert.NotEmpty(t, n.Name)
			assert.NotEmpty(t, n.States)
			assert.Nil(t, n.FreeMemoryMB)
		})
	}
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
