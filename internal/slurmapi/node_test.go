package slurmapi

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeFromAPI(t *testing.T) {
	tests := []struct {
		filename string
		want     Node
		wantErr  bool
	}{
		{
			filename: "testdata/usual_node_rest.json",
			want: Node{
				Name:        "worker-1",
				ClusterName: "",
				InstanceID:  "computeinstance-xxxxxxxxxxxxx",
				States: map[api.V0041NodeState]struct{}{
					api.V0041NodeStateIDLE:        {},
					api.V0041NodeStateDYNAMICNORM: {},
				},
				Reason:     nil,
				Partitions: []string{"main"},
				Tres:       "cpu=16,mem=191356M,billing=16,gres/gpu=1",
				Address:    "10.0.0.1",
				BootTime:   time.Unix(1747752894, 0),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			data, err := os.ReadFile(tt.filename)
			require.NoError(t, err)

			var apiNode api.V0041Node
			err = json.Unmarshal(data, &apiNode)
			require.NoError(t, err)

			got, err := NodeFromAPI(apiNode)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want.Name, got.Name)
			assert.Equal(t, tt.want.ClusterName, got.ClusterName)
			assert.Equal(t, tt.want.InstanceID, got.InstanceID)
			assert.Equal(t, tt.want.States, got.States)
			assert.Equal(t, tt.want.Partitions, got.Partitions)
			assert.Equal(t, tt.want.Tres, got.Tres)
			assert.Equal(t, tt.want.Address, got.Address)
			assert.Equal(t, tt.want.BootTime, got.BootTime)

			// Check reason handling
			if tt.want.Reason == nil {
				assert.Nil(t, got.Reason)
			} else {
				require.NotNil(t, got.Reason)
				assert.Equal(t, tt.want.Reason.Reason, got.Reason.Reason)
				assert.WithinDuration(t, tt.want.Reason.ChangedAt, got.Reason.ChangedAt, time.Second)
			}
		})
	}
}

func TestNode_IsNotUsable(t *testing.T) {
	tests := []struct {
		name     string
		states   map[api.V0041NodeState]struct{}
		expected bool
	}{
		{
			name: "DOWN state is not usable",
			states: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateDOWN: {},
			},
			expected: true,
		},
		{
			name: "DOWN+DRAIN state is not usable",
			states: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateDOWN:  {},
				api.V0041NodeStateDRAIN: {},
			},
			expected: true,
		},
		{
			name: "IDLE+DRAIN state is not usable",
			states: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateIDLE:  {},
				api.V0041NodeStateDRAIN: {},
			},
			expected: true,
		},
		{
			name: "IDLE+FAIL state is not usable",
			states: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateIDLE: {},
				api.V0041NodeStateFAIL: {},
			},
			expected: true,
		},
		{
			name: "IDLE+FAIL+DRAIN state is not usable",
			states: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateIDLE:  {},
				api.V0041NodeStateFAIL:  {},
				api.V0041NodeStateDRAIN: {},
			},
			expected: true,
		},
		{
			name: "IDLE state is usable",
			states: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateIDLE: {},
			},
			expected: false,
		},
		{
			name: "ALLOCATED state is usable",
			states: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateALLOCATED: {},
			},
			expected: false,
		},
		{
			name: "RUNNING+DRAIN state is usable (job still running)",
			states: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateALLOCATED: {}, // RUNNING is represented as ALLOCATED in API
				api.V0041NodeStateDRAIN:     {},
			},
			expected: false,
		},
		{
			name: "MIXED state is usable",
			states: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateMIXED: {},
			},
			expected: false,
		},
		{
			name: "MIXED+DRAIN state is usable",
			states: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateMIXED: {},
				api.V0041NodeStateDRAIN: {},
			},
			expected: false,
		},
		{
			name: "IDLE+MAINTENANCE state is usable",
			states: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateIDLE:        {},
				api.V0041NodeStateMAINTENANCE: {},
			},
			expected: false,
		},
		{
			name: "IDLE+RESERVED state is usable",
			states: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateIDLE:     {},
				api.V0041NodeStateRESERVED: {},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &Node{
				Name:   "test-node",
				States: tt.states,
			}

			result := node.IsNotUsable()
			assert.Equal(t, tt.expected, result, "IsNotUsable() should return %v for states %v", tt.expected, tt.states)
		})
	}
}
