package soperatorchecks

import (
	"context"
	"errors"
	"testing"
	"time"

	slurmapispec "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func Test_slurmWorkersController_findDegradedNodes(t *testing.T) {
	tests := []struct {
		name             string
		k8sNodeName      string
		slurmClusterName types.NamespacedName

		listNodesOut []slurmapi.Node
		listNodesErr error

		want    map[types.NamespacedName][]slurmapi.Node
		wantErr bool
	}{
		{
			name:        "happy-path/found-degraded-node",
			k8sNodeName: "k8s-node",
			slurmClusterName: types.NamespacedName{
				Namespace: "namespace",
				Name:      "name",
			},
			listNodesOut: []slurmapi.Node{
				{
					Name:        "node",
					ClusterName: "slurm-cluster",
					InstanceID:  "k8s-node",
					States: map[slurmapispec.V0041NodeState]struct{}{
						slurmapispec.V0041NodeStateDRAIN: {},
					},
					Reason: ptr.To(slurmapi.NodeReason{
						Reason:    consts.SlurmNodeReasonKillTaskFailed,
						ChangedAt: time.Date(2024, time.March, 1, 1, 1, 1, 1, time.UTC),
					}),
				},
			},
			listNodesErr: nil,
			want: map[types.NamespacedName][]slurmapi.Node{
				{
					Namespace: "namespace",
					Name:      "name",
				}: {
					{
						Name:        "node",
						ClusterName: "slurm-cluster",
						InstanceID:  "k8s-node",
						States: map[slurmapispec.V0041NodeState]struct{}{
							slurmapispec.V0041NodeStateDRAIN: {},
						},
						Reason: ptr.To(slurmapi.NodeReason{
							Reason:    consts.SlurmNodeReasonKillTaskFailed,
							ChangedAt: time.Date(2024, time.March, 1, 1, 1, 1, 1, time.UTC),
						}),
					},
				},
			},
			wantErr: false,
		},
		{
			name:        "unhappy-path/slurmapi-error",
			k8sNodeName: "k8s-node",
			slurmClusterName: types.NamespacedName{
				Namespace: "namespace",
				Name:      "name",
			},
			listNodesErr: errors.New("error"),
			wantErr:      true,
		},
		{
			name:        "happy-path/skip/different-k8s-node",
			k8sNodeName: "k8s-node-1",
			slurmClusterName: types.NamespacedName{
				Namespace: "namespace",
				Name:      "name",
			},
			listNodesOut: []slurmapi.Node{
				{
					Name:        "node",
					ClusterName: "slurm-cluster",
					InstanceID:  "k8s-node-2",
					States: map[slurmapispec.V0041NodeState]struct{}{
						slurmapispec.V0041NodeStateDRAIN: {},
					},
					Reason: ptr.To(slurmapi.NodeReason{
						Reason:    consts.SlurmNodeReasonKillTaskFailed,
						ChangedAt: time.Date(2024, time.March, 1, 1, 1, 1, 1, time.UTC),
					}),
				},
			},
			listNodesErr: nil,
			want:         map[types.NamespacedName][]slurmapi.Node{},
			wantErr:      false,
		},
		{
			name:        "happy-path/skip/not-drained",
			k8sNodeName: "k8s-node",
			slurmClusterName: types.NamespacedName{
				Namespace: "namespace",
				Name:      "name",
			},
			listNodesOut: []slurmapi.Node{
				{
					Name:        "node",
					ClusterName: "slurm-cluster",
					InstanceID:  "k8s-node",
					States: map[slurmapispec.V0041NodeState]struct{}{
						slurmapispec.V0041NodeStateIDLE: {},
					},
					Reason: ptr.To(slurmapi.NodeReason{
						Reason:    consts.SlurmNodeReasonKillTaskFailed,
						ChangedAt: time.Date(2024, time.March, 1, 1, 1, 1, 1, time.UTC),
					}),
				},
			},
			listNodesErr: nil,
			want:         map[types.NamespacedName][]slurmapi.Node{},
			wantErr:      false,
		},
		{
			name:        "happy-path/skip/no-drain-reason",
			k8sNodeName: "k8s-node",
			slurmClusterName: types.NamespacedName{
				Namespace: "namespace",
				Name:      "name",
			},
			listNodesOut: []slurmapi.Node{
				{
					Name:        "node",
					ClusterName: "slurm-cluster",
					InstanceID:  "k8s-node",
					States: map[slurmapispec.V0041NodeState]struct{}{
						slurmapispec.V0041NodeStateDRAIN: {},
					},
					Reason: nil,
				},
			},
			listNodesErr: nil,
			want:         map[types.NamespacedName][]slurmapi.Node{},
			wantErr:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			apiClient := slurmapifake.NewMockClient(t)
			apiClient.On("ListNodes", ctx).Return(tt.listNodesOut, tt.listNodesErr)

			c := &slurmWorkersController{
				slurmAPIClients: map[types.NamespacedName]slurmapi.Client{
					tt.slurmClusterName: apiClient,
				},
			}
			got, err := c.findDegradedNodes(ctx, tt.k8sNodeName)
			require.Equal(t, tt.wantErr, err != nil)
			if !tt.wantErr {
				require.EqualValues(t, tt.want, got)
			}
		})
	}
}

// TODO: more tests
