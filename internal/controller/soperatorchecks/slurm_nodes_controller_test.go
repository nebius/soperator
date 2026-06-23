package soperatorchecks

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

func TestSlurmNodesController_processSetUnhealthy_waitsForFullyDrainedSlurmNode(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name               string
		slurmNodeStates    map[api.V0041NodeState]struct{}
		wantHardwareIssues bool
	}{
		{
			name: "allocated drained node waits",
			slurmNodeStates: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateALLOCATED: {},
				api.V0041NodeStateDRAIN:     {},
			},
			wantHardwareIssues: false,
		},
		{
			name: "mixed drained node waits",
			slurmNodeStates: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateMIXED: {},
				api.V0041NodeStateDRAIN: {},
			},
			wantHardwareIssues: false,
		},
		{
			name: "completing idle drained node waits",
			slurmNodeStates: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateIDLE:       {},
				api.V0041NodeStateDRAIN:      {},
				api.V0041NodeStateCOMPLETING: {},
			},
			wantHardwareIssues: false,
		},
		{
			name: "idle drained node is marked unhealthy",
			slurmNodeStates: map[api.V0041NodeState]struct{}{
				api.V0041NodeStateIDLE:  {},
				api.V0041NodeStateDRAIN: {},
			},
			wantHardwareIssues: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller, k8sClient, slurmClusterName, k8sNode, slurmNode := newSlurmNodesControllerForUnhealthyTest(
				t,
				ctx,
				tt.slurmNodeStates,
				true,
			)

			err := controller.processSetUnhealthy(ctx, k8sNode, slurmClusterName, slurmNode)
			require.NoError(t, err)

			require.Equal(t, tt.wantHardwareIssues, hasHardwareIssuesSuspected(t, ctx, k8sClient, k8sNode.Name))
		})
	}
}

func TestSlurmNodesController_processHealthCheckFailed_withoutExtensiveCheckWaitsForFullyDrainedSlurmNode(t *testing.T) {
	ctx := context.Background()
	controller, k8sClient, slurmClusterName, k8sNode, slurmNode := newSlurmNodesControllerForUnhealthyTest(
		t,
		ctx,
		map[api.V0041NodeState]struct{}{
			api.V0041NodeStateALLOCATED: {},
			api.V0041NodeStateDRAIN:     {},
		},
		false,
	)

	err := controller.processHealthCheckFailed(ctx, k8sNode, slurmClusterName, slurmNode, slurmNode.Reason)
	require.NoError(t, err)
	require.False(t, hasHardwareIssuesSuspected(t, ctx, k8sClient, k8sNode.Name))
}

func TestSlurmNodesController_processSetUnhealthy_staleDrainStillUndrains(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	slurmClusterName := types.NamespacedName{Namespace: "test-ns", Name: "test-cluster"}
	drainTime := time.Date(2026, time.April, 7, 10, 0, 0, 0, time.UTC)
	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-k8s-node",
			CreationTimestamp: metav1.NewTime(drainTime.Add(time.Minute)),
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(k8sNode).
		Build()

	apiClient := slurmapifake.NewMockClient(t)
	apiClient.On(
		"SlurmV0041PostNodeWithResponse",
		ctx,
		"worker-0",
		mock.MatchedBy(func(body api.SlurmV0041PostNodeJSONRequestBody) bool {
			return body.State != nil &&
				len(*body.State) == 1 &&
				(*body.State)[0] == api.V0041UpdateNodeMsgStateRESUME &&
				body.Comment == nil
		}),
	).Return(&api.SlurmV0041PostNodeResponse{
		JSON200: &api.V0041OpenapiResp{
			Errors: &[]api.V0041OpenapiError{},
		},
	}, nil).Once()

	slurmAPIClients := slurmapi.NewClientSet()
	slurmAPIClients.AddClient(slurmClusterName, apiClient)
	controller := NewSlurmNodesController(
		k8sClient,
		scheme,
		record.NewFakeRecorder(10),
		slurmAPIClients,
		time.Minute,
		true,
		true,
		k8sClient,
		"",
	)

	err := controller.processSetUnhealthy(ctx, k8sNode, slurmClusterName, slurmapi.Node{
		Name:       "worker-0",
		InstanceID: k8sNode.Name,
		Comment:    "gpu health check failed",
		Reason: ptr.To(slurmapi.NodeReason{
			ChangedAt: drainTime,
		}),
	})
	require.NoError(t, err)
	require.False(t, hasHardwareIssuesSuspected(t, ctx, k8sClient, k8sNode.Name))
}

func newSlurmNodesControllerForUnhealthyTest(
	t *testing.T,
	ctx context.Context,
	slurmNodeStates map[api.V0041NodeState]struct{},
	enableExtensiveCheck bool,
) (*SlurmNodesController, ctrlclient.Client, types.NamespacedName, *corev1.Node, slurmapi.Node) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	slurmClusterName := types.NamespacedName{Namespace: "test-ns", Name: "test-cluster"}
	drainTime := time.Date(2026, time.April, 7, 10, 0, 0, 0, time.UTC)
	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-k8s-node",
			CreationTimestamp: metav1.NewTime(drainTime.Add(-time.Minute)),
		},
	}
	workerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: slurmClusterName.Namespace,
			Name:      "worker-0",
			Labels: map[string]string{
				consts.LabelInstanceKey: slurmClusterName.Name,
				consts.LabelWorkerKey:   consts.LabelWorkerValue,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: k8sNode.Name,
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(k8sNode, workerPod).
		WithStatusSubresource(k8sNode).
		WithIndex(&corev1.Pod{}, "spec.nodeName", func(obj ctrlclient.Object) []string {
			pod := obj.(*corev1.Pod)
			return []string{pod.Spec.NodeName}
		}).
		Build()

	apiClient := slurmapifake.NewMockClient(t)
	apiClient.On("GetNode", ctx, workerPod.Name).Return(slurmapi.Node{
		Name:   workerPod.Name,
		States: slurmNodeStates,
	}, nil).Once()

	slurmAPIClients := slurmapi.NewClientSet()
	slurmAPIClients.AddClient(slurmClusterName, apiClient)

	controller := NewSlurmNodesController(
		k8sClient,
		scheme,
		record.NewFakeRecorder(10),
		slurmAPIClients,
		time.Minute,
		true,
		enableExtensiveCheck,
		k8sClient,
		"",
	)

	return controller, k8sClient, slurmClusterName, k8sNode, slurmapi.Node{
		Name:       workerPod.Name,
		InstanceID: k8sNode.Name,
		Comment:    "gpu health check failed",
		Reason: ptr.To(slurmapi.NodeReason{
			ChangedAt: drainTime,
		}),
	}
}

func hasHardwareIssuesSuspected(t *testing.T, ctx context.Context, k8sClient ctrlclient.Client, nodeName string) bool {
	t.Helper()

	var node corev1.Node
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: nodeName}, &node))
	for _, cond := range node.Status.Conditions {
		if cond.Type == consts.HardwareIssuesSuspected {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}
