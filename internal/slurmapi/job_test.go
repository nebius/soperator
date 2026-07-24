package slurmapi

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func TestJobFromAPI(t *testing.T) {
	tests := []struct {
		name    string
		apiJob  api.V0041JobInfo
		want    Job
		wantErr bool
	}{
		{
			name: "complete job",
			apiJob: api.V0041JobInfo{
				JobId:          ptr.To(int32(12345)),
				Name:           ptr.To("test_job"),
				JobState:       &[]api.V0041JobInfoJobState{api.V0041JobInfoJobStateCOMPLETED},
				StateReason:    ptr.To("None"),
				Partition:      ptr.To("gpu"),
				UserName:       ptr.To("testuser"),
				StandardError:  ptr.To("/path/to/stderr"),
				StandardOutput: ptr.To("/path/to/stdout"),
				Nodes:          ptr.To("gpu[001-003]"),
				ScheduledNodes: ptr.To("gpu001,gpu002"),
				RequiredNodes:  ptr.To("gpu[001-005]"),
			},
			want: Job{
				ID:             12345,
				Name:           "test_job",
				State:          "COMPLETED",
				StateReason:    "None",
				Partition:      "gpu",
				UserName:       "testuser",
				StandardError:  "/path/to/stderr",
				StandardOutput: "/path/to/stdout",
				Nodes:          "gpu[001-003]",
				ScheduledNodes: "gpu001,gpu002",
				RequiredNodes:  "gpu[001-005]",
			},
			wantErr: false,
		},
		{
			name: "minimal job",
			apiJob: api.V0041JobInfo{
				JobId:    ptr.To(int32(123)),
				JobState: &[]api.V0041JobInfoJobState{api.V0041JobInfoJobStateCOMPLETED},
			},
			want: Job{
				ID:    123,
				State: "COMPLETED",
			},
			wantErr: false,
		},
		{
			name: "job without ID",
			apiJob: api.V0041JobInfo{
				Name: ptr.To("test"),
			},
			want:    Job{},
			wantErr: true,
		},
		{
			name: "job without State",
			apiJob: api.V0041JobInfo{
				JobId: ptr.To(int32(123)),
			},
			want:    Job{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := JobFromAPI(tt.apiJob)
			if (err != nil) != tt.wantErr {
				t.Errorf("JobFromAPI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("JobFromAPI() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobFromAccountingAPI(t *testing.T) {
	tests := []struct {
		name    string
		apiJob  api.V0041Job
		want    Job
		wantErr bool
	}{
		{
			name: "complete accounting job",
			apiJob: api.V0041Job{
				JobId:     ptr.To(int32(54321)),
				Name:      ptr.To("accounting_job"),
				Partition: ptr.To("main"),
				User:      ptr.To("testuser"),
				Nodes:     ptr.To("worker-[1,2]"),
				State: &struct {
					Current *[]api.V0041JobStateCurrent `json:"current,omitempty"`
					Reason  *string                     `json:"reason,omitempty"`
				}{
					Current: &[]api.V0041JobStateCurrent{api.V0041JobStateCurrentRUNNING},
					Reason:  ptr.To("None"),
				},
				Stderr: ptr.To("/tmp/stderr"),
				Stdout: ptr.To("/tmp/stdout"),
				Array: &struct {
					JobId  *int32 `json:"job_id,omitempty"`
					Limits *struct {
						Max *struct {
							Running *struct {
								Tasks *int32 `json:"tasks,omitempty"`
							} `json:"running,omitempty"`
						} `json:"max,omitempty"`
					} `json:"limits,omitempty"`
					Task   *string                     `json:"task,omitempty"`
					TaskId *api.V0041Uint32NoValStruct `json:"task_id,omitempty"`
				}{
					JobId: ptr.To(int32(54000)),
					TaskId: &api.V0041Uint32NoValStruct{
						Set:    ptr.To(true),
						Number: ptr.To(int32(7)),
					},
				},
				AllocationNodes: ptr.To(int32(2)),
				Required: &struct {
					CPUs          *int32                      `json:"CPUs,omitempty"`
					MemoryPerCpu  *api.V0041Uint64NoValStruct `json:"memory_per_cpu,omitempty"`
					MemoryPerNode *api.V0041Uint64NoValStruct `json:"memory_per_node,omitempty"`
				}{
					CPUs: ptr.To(int32(8)),
					MemoryPerNode: &api.V0041Uint64NoValStruct{
						Set:    ptr.To(true),
						Number: ptr.To(int64(32768)),
					},
				},
				Time: &struct {
					Elapsed    *int32                      `json:"elapsed,omitempty"`
					Eligible   *int64                      `json:"eligible,omitempty"`
					End        *int64                      `json:"end,omitempty"`
					Limit      *api.V0041Uint32NoValStruct `json:"limit,omitempty"`
					Planned    *api.V0041Uint64NoValStruct `json:"planned,omitempty"`
					Start      *int64                      `json:"start,omitempty"`
					Submission *int64                      `json:"submission,omitempty"`
					Suspended  *int32                      `json:"suspended,omitempty"`
					System     *struct {
						Microseconds *int64 `json:"microseconds,omitempty"`
						Seconds      *int64 `json:"seconds,omitempty"`
					} `json:"system,omitempty"`
					Total *struct {
						Microseconds *int64 `json:"microseconds,omitempty"`
						Seconds      *int64 `json:"seconds,omitempty"`
					} `json:"total,omitempty"`
					User *struct {
						Microseconds *int64 `json:"microseconds,omitempty"`
						Seconds      *int64 `json:"seconds,omitempty"`
					} `json:"user,omitempty"`
				}{
					Submission: ptr.To(int64(1722697200)),
					Start:      ptr.To(int64(1722697230)),
					End:        ptr.To(int64(1722697290)),
				},
			},
			want: Job{
				ID:             54321,
				Name:           "accounting_job",
				State:          "RUNNING",
				StateReason:    "None",
				Partition:      "main",
				UserName:       "testuser",
				StandardError:  "/tmp/stderr",
				StandardOutput: "/tmp/stdout",
				Nodes:          "worker-[1,2]",
				NodeCount:      ptr.To(int32(2)),
				ArrayJobID:     ptr.To(int32(54000)),
				ArrayTaskID:    ptr.To(int32(7)),
				CPUs:           ptr.To(int32(8)),
				MemoryPerNode:  ptr.To(int64(32768)),
			},
			wantErr: false,
		},
		{
			name: "job without state",
			apiJob: api.V0041Job{
				JobId: ptr.To(int32(123)),
			},
			wantErr: true,
		},
		{
			name: "user falls back to association user",
			apiJob: api.V0041Job{
				JobId: ptr.To(int32(123)),
				Group: ptr.To("group-name"),
				Association: &api.V0041AssocShort{
					User: "association-user",
				},
				State: &struct {
					Current *[]api.V0041JobStateCurrent `json:"current,omitempty"`
					Reason  *string                     `json:"reason,omitempty"`
				}{
					Current: &[]api.V0041JobStateCurrent{api.V0041JobStateCurrentRUNNING},
				},
				Time: &struct {
					Elapsed    *int32                      `json:"elapsed,omitempty"`
					Eligible   *int64                      `json:"eligible,omitempty"`
					End        *int64                      `json:"end,omitempty"`
					Limit      *api.V0041Uint32NoValStruct `json:"limit,omitempty"`
					Planned    *api.V0041Uint64NoValStruct `json:"planned,omitempty"`
					Start      *int64                      `json:"start,omitempty"`
					Submission *int64                      `json:"submission,omitempty"`
					Suspended  *int32                      `json:"suspended,omitempty"`
					System     *struct {
						Microseconds *int64 `json:"microseconds,omitempty"`
						Seconds      *int64 `json:"seconds,omitempty"`
					} `json:"system,omitempty"`
					Total *struct {
						Microseconds *int64 `json:"microseconds,omitempty"`
						Seconds      *int64 `json:"seconds,omitempty"`
					} `json:"total,omitempty"`
					User *struct {
						Microseconds *int64 `json:"microseconds,omitempty"`
						Seconds      *int64 `json:"seconds,omitempty"`
					} `json:"user,omitempty"`
				}{
					Submission: ptr.To(int64(1722697200)),
					Start:      ptr.To(int64(1722697230)),
					End:        ptr.To(int64(1722697290)),
				},
			},
			want: Job{
				ID:       123,
				State:    "RUNNING",
				UserName: "association-user",
			},
			wantErr: false,
		},
		{
			name: "pending job leaves NodeCount nil",
			apiJob: api.V0041Job{
				JobId: ptr.To(int32(125)),
				State: &struct {
					Current *[]api.V0041JobStateCurrent `json:"current,omitempty"`
					Reason  *string                     `json:"reason,omitempty"`
				}{
					Current: &[]api.V0041JobStateCurrent{api.V0041JobStateCurrentPENDING},
				},
				AllocationNodes: ptr.To(int32(0)),
				Required: &struct {
					CPUs          *int32                      `json:"CPUs,omitempty"`
					MemoryPerCpu  *api.V0041Uint64NoValStruct `json:"memory_per_cpu,omitempty"`
					MemoryPerNode *api.V0041Uint64NoValStruct `json:"memory_per_node,omitempty"`
				}{
					CPUs: ptr.To(int32(4)),
					MemoryPerNode: &api.V0041Uint64NoValStruct{
						Set:    ptr.To(true),
						Number: ptr.To(int64(8192)),
					},
				},
				Time: &struct {
					Elapsed    *int32                      `json:"elapsed,omitempty"`
					Eligible   *int64                      `json:"eligible,omitempty"`
					End        *int64                      `json:"end,omitempty"`
					Limit      *api.V0041Uint32NoValStruct `json:"limit,omitempty"`
					Planned    *api.V0041Uint64NoValStruct `json:"planned,omitempty"`
					Start      *int64                      `json:"start,omitempty"`
					Submission *int64                      `json:"submission,omitempty"`
					Suspended  *int32                      `json:"suspended,omitempty"`
					System     *struct {
						Microseconds *int64 `json:"microseconds,omitempty"`
						Seconds      *int64 `json:"seconds,omitempty"`
					} `json:"system,omitempty"`
					Total *struct {
						Microseconds *int64 `json:"microseconds,omitempty"`
						Seconds      *int64 `json:"seconds,omitempty"`
					} `json:"total,omitempty"`
					User *struct {
						Microseconds *int64 `json:"microseconds,omitempty"`
						Seconds      *int64 `json:"seconds,omitempty"`
					} `json:"user,omitempty"`
				}{
					Submission: ptr.To(int64(1722697200)),
					Start:      ptr.To(int64(1722697230)),
					End:        ptr.To(int64(1722697290)),
				},
			},
			want: Job{
				ID:            125,
				State:         "PENDING",
				NodeCount:     nil,
				CPUs:          ptr.To(int32(4)),
				MemoryPerNode: ptr.To(int64(8192)),
			},
			wantErr: false,
		},
		{
			name: "tres fields",
			apiJob: api.V0041Job{
				JobId: ptr.To(int32(124)),
				State: &struct {
					Current *[]api.V0041JobStateCurrent `json:"current,omitempty"`
					Reason  *string                     `json:"reason,omitempty"`
				}{
					Current: &[]api.V0041JobStateCurrent{api.V0041JobStateCurrentRUNNING},
				},
				Tres: &struct {
					Allocated *api.V0041TresList `json:"allocated,omitempty"`
					Requested *api.V0041TresList `json:"requested,omitempty"`
				}{
					Allocated: &api.V0041TresList{
						{Type: "cpu", Count: ptr.To(int64(4))},
						{Type: "mem", Count: ptr.To(int64(8192))},
						{Type: "gres", Name: ptr.To("gpu"), Count: ptr.To(int64(2))},
					},
					Requested: &api.V0041TresList{
						{Type: "mem", Count: ptr.To(int64(8192))},
					},
				},
				Time: &struct {
					Elapsed    *int32                      `json:"elapsed,omitempty"`
					Eligible   *int64                      `json:"eligible,omitempty"`
					End        *int64                      `json:"end,omitempty"`
					Limit      *api.V0041Uint32NoValStruct `json:"limit,omitempty"`
					Planned    *api.V0041Uint64NoValStruct `json:"planned,omitempty"`
					Start      *int64                      `json:"start,omitempty"`
					Submission *int64                      `json:"submission,omitempty"`
					Suspended  *int32                      `json:"suspended,omitempty"`
					System     *struct {
						Microseconds *int64 `json:"microseconds,omitempty"`
						Seconds      *int64 `json:"seconds,omitempty"`
					} `json:"system,omitempty"`
					Total *struct {
						Microseconds *int64 `json:"microseconds,omitempty"`
						Seconds      *int64 `json:"seconds,omitempty"`
					} `json:"total,omitempty"`
					User *struct {
						Microseconds *int64 `json:"microseconds,omitempty"`
						Seconds      *int64 `json:"seconds,omitempty"`
					} `json:"user,omitempty"`
				}{
					Submission: ptr.To(int64(1722697200)),
					Start:      ptr.To(int64(1722697230)),
					End:        ptr.To(int64(1722697290)),
				},
			},
			want: Job{
				ID:            124,
				State:         "RUNNING",
				TresAllocated: "cpu=4,mem=8192M,gres/gpu=2",
				TresRequested: "mem=8192M",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := JobFromAccountingAPI(tt.apiJob)
			if (err != nil) != tt.wantErr {
				t.Fatalf("JobFromAccountingAPI() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			assert.Equal(t, tt.want.ID, got.ID)
			assert.Equal(t, tt.want.Name, got.Name)
			assert.Equal(t, tt.want.State, got.State)
			assert.Equal(t, tt.want.StateReason, got.StateReason)
			assert.Equal(t, tt.want.Partition, got.Partition)
			assert.Equal(t, tt.want.UserName, got.UserName)
			assert.Equal(t, tt.want.StandardError, got.StandardError)
			assert.Equal(t, tt.want.StandardOutput, got.StandardOutput)
			assert.Equal(t, tt.want.Nodes, got.Nodes)
			assert.Equal(t, tt.want.NodeCount, got.NodeCount)
			assert.Equal(t, tt.want.ArrayJobID, got.ArrayJobID)
			assert.Equal(t, tt.want.ArrayTaskID, got.ArrayTaskID)
			assert.Equal(t, tt.want.CPUs, got.CPUs)
			assert.Equal(t, tt.want.MemoryPerNode, got.MemoryPerNode)
			assert.Equal(t, tt.want.TresAllocated, got.TresAllocated)
			assert.Equal(t, tt.want.TresRequested, got.TresRequested)
			require.NotNil(t, got.SubmitTime)
			require.NotNil(t, got.StartTime)
			require.NotNil(t, got.EndTime)
			assert.Equal(t, int64(1722697200), got.SubmitTime.Unix())
			assert.Equal(t, int64(1722697230), got.StartTime.Unix())
			assert.Equal(t, int64(1722697290), got.EndTime.Unix())
		})
	}
}

// runningJobWithTimeLimit constructs a minimal V0041Job in RUNNING state with the given start
// timestamp and time-limit shape so the projection-related assertions stay focused.
func runningJobWithTimeLimit(t *testing.T, start int64, limit *api.V0041Uint32NoValStruct) api.V0041Job {
	t.Helper()
	return api.V0041Job{
		JobId: ptr.To(int32(900)),
		State: &struct {
			Current *[]api.V0041JobStateCurrent `json:"current,omitempty"`
			Reason  *string                     `json:"reason,omitempty"`
		}{
			Current: &[]api.V0041JobStateCurrent{api.V0041JobStateCurrentRUNNING},
		},
		Time: &struct {
			Elapsed    *int32                      `json:"elapsed,omitempty"`
			Eligible   *int64                      `json:"eligible,omitempty"`
			End        *int64                      `json:"end,omitempty"`
			Limit      *api.V0041Uint32NoValStruct `json:"limit,omitempty"`
			Planned    *api.V0041Uint64NoValStruct `json:"planned,omitempty"`
			Start      *int64                      `json:"start,omitempty"`
			Submission *int64                      `json:"submission,omitempty"`
			Suspended  *int32                      `json:"suspended,omitempty"`
			System     *struct {
				Microseconds *int64 `json:"microseconds,omitempty"`
				Seconds      *int64 `json:"seconds,omitempty"`
			} `json:"system,omitempty"`
			Total *struct {
				Microseconds *int64 `json:"microseconds,omitempty"`
				Seconds      *int64 `json:"seconds,omitempty"`
			} `json:"total,omitempty"`
			User *struct {
				Microseconds *int64 `json:"microseconds,omitempty"`
				Seconds      *int64 `json:"seconds,omitempty"`
			} `json:"user,omitempty"`
		}{
			Start: &start,
			End:   ptr.To(int64(0)),
			Limit: limit,
		},
	}
}

func TestJobFromAccountingAPI_RunningProjectsEndTime(t *testing.T) {
	const start = int64(1722697230)
	const limitMinutes = int32(120)

	apiJob := runningJobWithTimeLimit(t, start, &api.V0041Uint32NoValStruct{
		Set:    ptr.To(true),
		Number: ptr.To(limitMinutes),
	})

	got, err := JobFromAccountingAPI(apiJob)
	require.NoError(t, err)

	require.NotNil(t, got.EndTime)
	assert.Equal(t, start+int64(limitMinutes)*60, got.EndTime.Unix())
}

func TestJobFromAccountingAPI_RunningWithUnlimitedTimeLeavesEndNil(t *testing.T) {
	apiJob := runningJobWithTimeLimit(t, int64(1722697230), &api.V0041Uint32NoValStruct{
		Infinite: ptr.To(true),
	})

	got, err := JobFromAccountingAPI(apiJob)
	require.NoError(t, err)
	assert.Nil(t, got.EndTime)
}

func TestJobFromAccountingAPI_RunningWithUnsetTimeLimitLeavesEndNil(t *testing.T) {
	apiJob := runningJobWithTimeLimit(t, int64(1722697230), nil)

	got, err := JobFromAccountingAPI(apiJob)
	require.NoError(t, err)
	assert.Nil(t, got.EndTime)
}

func TestJobFromAccountingAPI_NormalizesUnallocatedNodes(t *testing.T) {
	tests := []struct {
		name  string
		nodes string
		want  string
	}{
		{name: "None assigned", nodes: "None assigned", want: ""},
		{name: "(null)", nodes: "(null)", want: ""},
		{name: "whitespace", nodes: "   ", want: ""},
		{name: "real allocation passes through", nodes: "worker-[1,2]", want: "worker-[1,2]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiJob := api.V0041Job{
				JobId: ptr.To(int32(901)),
				State: &struct {
					Current *[]api.V0041JobStateCurrent `json:"current,omitempty"`
					Reason  *string                     `json:"reason,omitempty"`
				}{
					Current: &[]api.V0041JobStateCurrent{api.V0041JobStateCurrentPENDING},
				},
				Nodes: ptr.To(tt.nodes),
			}
			got, err := JobFromAccountingAPI(apiJob)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Nodes)
		})
	}
}

func TestIsStaleAccountingPending(t *testing.T) {
	mkJob := func(start, end, submit *int64) api.V0041Job {
		return api.V0041Job{
			Time: &struct {
				Elapsed    *int32                      `json:"elapsed,omitempty"`
				Eligible   *int64                      `json:"eligible,omitempty"`
				End        *int64                      `json:"end,omitempty"`
				Limit      *api.V0041Uint32NoValStruct `json:"limit,omitempty"`
				Planned    *api.V0041Uint64NoValStruct `json:"planned,omitempty"`
				Start      *int64                      `json:"start,omitempty"`
				Submission *int64                      `json:"submission,omitempty"`
				Suspended  *int32                      `json:"suspended,omitempty"`
				System     *struct {
					Microseconds *int64 `json:"microseconds,omitempty"`
					Seconds      *int64 `json:"seconds,omitempty"`
				} `json:"system,omitempty"`
				Total *struct {
					Microseconds *int64 `json:"microseconds,omitempty"`
					Seconds      *int64 `json:"seconds,omitempty"`
				} `json:"total,omitempty"`
				User *struct {
					Microseconds *int64 `json:"microseconds,omitempty"`
					Seconds      *int64 `json:"seconds,omitempty"`
				} `json:"user,omitempty"`
			}{Start: start, End: end, Submission: submit},
		}
	}

	now := time.Now().Unix()
	cutoff := now - 24*60*60 // 24h ago

	tests := []struct {
		name string
		job  api.V0041Job
		want bool
	}{
		{name: "no Time field", job: api.V0041Job{}, want: false},
		{name: "all zero", job: mkJob(ptr.To(int64(0)), ptr.To(int64(0)), ptr.To(int64(0))), want: false},
		{name: "submitted within window, never started", job: mkJob(ptr.To(int64(0)), ptr.To(int64(0)), ptr.To(now-60*60)), want: false},
		{name: "submitted long ago, never started, never ended → stale zombie", job: mkJob(ptr.To(int64(0)), ptr.To(int64(0)), ptr.To(now-48*60*60)), want: true},
		{name: "actually started → not zombie", job: mkJob(ptr.To(now-24*60*60), ptr.To(int64(0)), ptr.To(now-48*60*60)), want: false},
		{name: "actually ended → not zombie", job: mkJob(ptr.To(int64(0)), ptr.To(now-60*60), ptr.To(now-48*60*60)), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isStaleAccountingPending(tt.job, cutoff))
		})
	}
}

func TestIsUnallocatedNodeList(t *testing.T) {
	for _, s := range []string{"", " ", "   ", "None assigned", "(null)"} {
		assert.True(t, isUnallocatedNodeList(s), "expected %q to be unallocated", s)
	}
	for _, s := range []string{"worker-1", "worker-[1,2]", "node[01-04]"} {
		assert.False(t, isUnallocatedNodeList(s), "expected %q to be allocated", s)
	}
}

func TestJobFromAPI_SmokeTest(t *testing.T) {
	tests := []struct {
		filename string
		want     Job
		wantErr  bool
	}{
		{
			filename: "testdata/2_node_job.json",
			want: Job{
				ID:             349,
				Name:           "wrap",
				State:          "COMPLETED",
				StateReason:    "None",
				Partition:      "main",
				UserName:       "root",
				StandardError:  "/root/slurm-349.out",
				StandardOutput: "/root/slurm-349.out",
				Nodes:          "worker-[1,0]",
				ScheduledNodes: "",
				RequiredNodes:  "",
				NodeCount:      ptr.To(int32(2)),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			data, err := os.ReadFile(tt.filename)
			require.NoError(t, err)

			var apiJob api.V0041JobInfo
			err = json.Unmarshal(data, &apiJob)
			require.NoError(t, err)

			got, err := JobFromAPI(apiJob)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want.ID, got.ID)
			assert.Equal(t, tt.want.Name, got.Name)
			assert.Equal(t, tt.want.State, got.State)
			assert.Equal(t, tt.want.StateReason, got.StateReason)
			assert.Equal(t, tt.want.Partition, got.Partition)
			assert.Equal(t, tt.want.UserName, got.UserName)
			assert.Equal(t, tt.want.StandardError, got.StandardError)
			assert.Equal(t, tt.want.StandardOutput, got.StandardOutput)
			assert.Equal(t, tt.want.Nodes, got.Nodes)
			assert.Equal(t, tt.want.ScheduledNodes, got.ScheduledNodes)
			assert.Equal(t, tt.want.RequiredNodes, got.RequiredNodes)
			assert.Equal(t, tt.want.NodeCount, got.NodeCount)

			// Test node list methods
			nodeList, err := got.GetNodeList()
			require.NoError(t, err)
			assert.Equal(t, []string{"worker-1", "worker-0"}, nodeList)

			scheduledNodeList, err := got.GetScheduledNodeList()
			require.NoError(t, err)
			assert.Equal(t, []string(nil), scheduledNodeList)

			requiredNodeList, err := got.GetRequiredNodeList()
			require.NoError(t, err)
			assert.Equal(t, []string(nil), requiredNodeList)
		})
	}
}

func TestJob_GetNodeList(t *testing.T) {
	tests := []struct {
		name     string
		job      Job
		expected []string
	}{
		{
			name:     "range expansion",
			job:      Job{Nodes: "gpu[001-003]"},
			expected: []string{"gpu001", "gpu002", "gpu003"},
		},
		{
			name:     "mixed range and individual",
			job:      Job{Nodes: "gpu[001-002],worker005"},
			expected: []string{"gpu001", "gpu002", "worker005"},
		},
		{
			name:     "simple comma separated",
			job:      Job{Nodes: "node1,node2,node3"},
			expected: []string{"node1", "node2", "node3"},
		},
		{
			name:     "single node",
			job:      Job{Nodes: "worker-0"},
			expected: []string{"worker-0"},
		},
		{
			name:     "empty nodes",
			job:      Job{Nodes: ""},
			expected: nil,
		},
		{
			name:     "worker-[1,0] pattern from real data",
			job:      Job{Nodes: "worker-[1,0]"},
			expected: []string{"worker-1", "worker-0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.job.GetNodeList()
			if err != nil {
				t.Errorf("GetNodeList() error = %v", err)
				return
			}
			if len(got) != len(tt.expected) {
				t.Errorf("GetNodeList() = %v, want %v", got, tt.expected)
				return
			}
			for i, node := range got {
				if node != tt.expected[i] {
					t.Errorf("GetNodeList()[%d] = %v, want %v", i, node, tt.expected[i])
				}
			}
		})
	}
}

func TestJob_GetNodeList_Error(t *testing.T) {
	tests := []struct {
		name    string
		job     Job
		wantErr bool
	}{
		{
			name:    "invalid range format",
			job:     Job{Nodes: "gpu[001-002-003]"},
			wantErr: true,
		},
		{
			name:    "invalid start number",
			job:     Job{Nodes: "gpu[abc-003]"},
			wantErr: true,
		},
		{
			name:    "invalid end number",
			job:     Job{Nodes: "gpu[001-xyz]"},
			wantErr: true,
		},
		{
			name:    "start greater than end",
			job:     Job{Nodes: "gpu[005-001]"},
			wantErr: true,
		},
		{
			name:    "invalid individual number",
			job:     Job{Nodes: "gpu[abc]"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.job.GetNodeList()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetNodeList() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestJob_GetIDString(t *testing.T) {
	job := Job{ID: 12345}
	want := "12345"
	if got := job.GetIDString(); got != want {
		t.Errorf("Job.GetIDString() = %v, want %v", got, want)
	}
}

func TestJob_GetArrayTaskIDString(t *testing.T) {
	tests := []struct {
		name string
		job  Job
		want string
	}{
		{name: "non-array job", job: Job{}, want: ""},
		{name: "exploded task wins", job: Job{ArrayTaskID: ptr.To(int32(3)), ArrayTaskString: "1-5"}, want: "3"},
		{name: "exploded task without range", job: Job{ArrayTaskID: ptr.To(int32(7))}, want: "7"},
		{name: "collapsed master record falls back to range", job: Job{ArrayTaskString: "1-5"}, want: "1-5"},
		{name: "complex range expression", job: Job{ArrayTaskString: "1,3,5-9:2"}, want: "1,3,5-9:2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.job.GetArrayTaskIDString())
		})
	}
}

func TestJobFromAccountingAPI_PreservesArrayTaskString(t *testing.T) {
	apiJob := api.V0041Job{
		JobId: ptr.To(int32(901)),
		State: &struct {
			Current *[]api.V0041JobStateCurrent `json:"current,omitempty"`
			Reason  *string                     `json:"reason,omitempty"`
		}{
			Current: &[]api.V0041JobStateCurrent{api.V0041JobStateCurrentPENDING},
		},
		Array: &struct {
			JobId  *int32 `json:"job_id,omitempty"`
			Limits *struct {
				Max *struct {
					Running *struct {
						Tasks *int32 `json:"tasks,omitempty"`
					} `json:"running,omitempty"`
				} `json:"max,omitempty"`
			} `json:"limits,omitempty"`
			Task   *string                     `json:"task,omitempty"`
			TaskId *api.V0041Uint32NoValStruct `json:"task_id,omitempty"`
		}{
			JobId: ptr.To(int32(900)),
			Task:  ptr.To("1-5"),
			// TaskId intentionally unset — slurmdbd's master-record shape.
		},
	}
	got, err := JobFromAccountingAPI(apiJob)
	require.NoError(t, err)
	assert.Nil(t, got.ArrayTaskID)
	assert.Equal(t, "1-5", got.ArrayTaskString)
	assert.Equal(t, "1-5", got.GetArrayTaskIDString())
}
