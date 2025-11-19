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
				Comment:    "comment",
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
			assert.Equal(t, tt.want.Comment, got.Comment)

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
