package soperatorchecks

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func Test_SlurmNodesController_findDegradedNodes(t *testing.T) {
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
					States: map[api.V0041NodeState]struct{}{
						api.V0041NodeStateDRAIN: {},
					},
					Reason: ptr.To(slurmapi.NodeReason{
						Reason:    fmt.Sprintf("%s: extra", consts.SlurmNodeReasonKillTaskFailed),
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
						States: map[api.V0041NodeState]struct{}{
							api.V0041NodeStateDRAIN: {},
						},
						Reason: ptr.To(slurmapi.NodeReason{
							Reason:         consts.SlurmNodeReasonKillTaskFailed,
							OriginalReason: fmt.Sprintf("%s: extra", consts.SlurmNodeReasonKillTaskFailed),
							ChangedAt:      time.Date(2024, time.March, 1, 1, 1, 1, 1, time.UTC),
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
					States: map[api.V0041NodeState]struct{}{
						api.V0041NodeStateIDLE: {},
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
					States: map[api.V0041NodeState]struct{}{
						api.V0041NodeStateDRAIN: {},
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

			slurmAPIClients := slurmapi.NewClientSet()
			slurmAPIClients.AddClient(tt.slurmClusterName, apiClient)
			c := &SlurmNodesController{
				slurmAPIClients: slurmAPIClients,
			}
			got, err := c.findDegradedNodes(ctx)
			require.Equal(t, tt.wantErr, err != nil)
			if !tt.wantErr {
				require.EqualValues(t, tt.want, got)
			}
		})
	}
}

func TestSlurmNodesController_processDegradedNode_KillTaskFailedDoesNotMarkNodeNeedReboot(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "k8s-node",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(k8sNode).
		Build()

	controller := NewSlurmNodesController(
		client,
		scheme,
		record.NewFakeRecorder(10),
		slurmapi.NewClientSet(),
		time.Minute,
		false,
		false,
		client,
		consts.DefaultMaintenanceConditionType,
	)

	err := controller.processDegradedNode(ctx, types.NamespacedName{
		Namespace: "namespace",
		Name:      "slurm-cluster",
	}, slurmapi.Node{
		Name:       "worker-0",
		InstanceID: k8sNode.Name,
		Reason: ptr.To(slurmapi.NodeReason{
			Reason: consts.SlurmNodeReasonKillTaskFailed,
		}),
	})
	require.NoError(t, err)

	var updatedNode corev1.Node
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: k8sNode.Name}, &updatedNode))

	for _, condition := range updatedNode.Status.Conditions {
		require.NotEqual(t, consts.SoperatorChecksK8SNodeDegraded, condition.Type)
		require.NotEqual(t, string(consts.ReasonNodeNeedReboot), condition.Reason)
	}
}

func TestToCamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "helloWorld"},
		{"FOO BAR", "fooBar"},
		{"multiple   spaces", "multipleSpaces"},
		{"unicode test", "unicodeTest"},
		{"123 numbers", "numbers"},
		{"special_characters!", "specialCharacters"},
		{"camelCaseAlready", "camelCaseAlready"},
		{"", ""},
	}

	for _, test := range tests {
		result := toCamelCase(test.input)
		if result != test.expected {
			t.Errorf("toCamelCase(%q) = %q; want %q", test.input, result, test.expected)
		}
	}
}
