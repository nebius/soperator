package slurmapi

import (
	"encoding/json"
	"os"
	"testing"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				JobId:          ptr(int32(12345)),
				Name:           ptr("test_job"),
				JobState:       &[]api.V0041JobInfoJobState{api.V0041JobInfoJobStateCOMPLETED},
				StateReason:    ptr("None"),
				Partition:      ptr("gpu"),
				UserName:       ptr("testuser"),
				StandardError:  ptr("/path/to/stderr"),
				StandardOutput: ptr("/path/to/stdout"),
				Nodes:          ptr("gpu[001-003]"),
				ScheduledNodes: ptr("gpu001,gpu002"),
				RequiredNodes:  ptr("gpu[001-005]"),
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
				JobId:    ptr(int32(123)),
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
				Name: ptr("test"),
			},
			want:    Job{},
			wantErr: true,
		},
		{
			name: "job without State",
			apiJob: api.V0041JobInfo{
				JobId: ptr(int32(123)),
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
				NodeCount:      ptr(int32(2)),
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

func ptr[T any](v T) *T {
	return &v
}
