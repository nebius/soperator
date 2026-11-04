package soperatorchecks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
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
	ctrl "sigs.k8s.io/controller-runtime"
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
					States: map[api.V0044NodeState]struct{}{
						api.V0044NodeStateDRAIN: {},
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
						States: map[api.V0044NodeState]struct{}{
							api.V0044NodeStateDRAIN: {},
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
					States: map[api.V0044NodeState]struct{}{
						api.V0044NodeStateIDLE: {},
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
					States: map[api.V0044NodeState]struct{}{
						api.V0044NodeStateDRAIN: {},
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

			slurmAPIClients := slurmapi.NewClientSet(context.Background())
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
		states             []api.V0044NodeState
		otherWorker        *slurmapi.Node
		wantHardwareIssues bool
	}{
		{
			name:   "allocated drained node waits",
			states: []api.V0044NodeState{api.V0044NodeStateALLOCATED, api.V0044NodeStateDRAIN},
		},
		{
			name:   "mixed drained node waits",
			states: []api.V0044NodeState{api.V0044NodeStateMIXED, api.V0044NodeStateDRAIN},
		},
		{
			name:   "completing idle drained node waits",
			states: []api.V0044NodeState{api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN, api.V0044NodeStateCOMPLETING},
		},
		{
			name:               "idle drained node is marked unhealthy",
			states:             []api.V0044NodeState{api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN},
			wantHardwareIssues: true,
		},
		{
			name:               "down drained node is marked unhealthy",
			states:             []api.V0044NodeState{api.V0044NodeStateDOWN, api.V0044NodeStateDRAIN},
			wantHardwareIssues: true,
		},
		{
			name:   "down undrained node waits",
			states: []api.V0044NodeState{api.V0044NodeStateDOWN},
		},
		{
			name:   "completing down drained node waits",
			states: []api.V0044NodeState{api.V0044NodeStateDOWN, api.V0044NodeStateDRAIN, api.V0044NodeStateCOMPLETING},
		},
		{
			name:               "down drained reboot requested node is marked unhealthy",
			states:             []api.V0044NodeState{api.V0044NodeStateDOWN, api.V0044NodeStateDRAIN, api.V0044NodeStateREBOOTREQUESTED},
			wantHardwareIssues: true,
		},
		{
			name:               "down drained reboot issued node is marked unhealthy",
			states:             []api.V0044NodeState{api.V0044NodeStateDOWN, api.V0044NodeStateDRAIN, api.V0044NodeStateREBOOTISSUED},
			wantHardwareIssues: true,
		},
		{
			name:   "another allocated worker on the host blocks maintenance",
			states: []api.V0044NodeState{api.V0044NodeStateDOWN, api.V0044NodeStateDRAIN},
			otherWorker: &slurmapi.Node{
				States: map[api.V0044NodeState]struct{}{
					api.V0044NodeStateALLOCATED: {},
					api.V0044NodeStateDRAIN:     {},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := slurmapi.Node{States: make(map[api.V0044NodeState]struct{}, len(tt.states))}
			for _, state := range tt.states {
				node.States[state] = struct{}{}
			}
			var slurmNodes []slurmapi.Node
			slurmNodes = append(slurmNodes, node)
			if tt.otherWorker != nil {
				slurmNodes = append(slurmNodes, *tt.otherWorker)
			}
			controller, k8sClient, slurmClusterName, k8sNode, slurmNode := newSlurmNodesControllerForUnhealthyTest(
				t,
				ctx,
				slurmNodes...,
			)

			err := controller.processSetUnhealthy(ctx, k8sNode, slurmClusterName, slurmNode)
			require.NoError(t, err)

			require.Equal(t, tt.wantHardwareIssues, hasHardwareIssuesSuspected(t, ctx, k8sClient, k8sNode.Name))
		})
	}
}

// The platform replaces a node with HardwareIssuesSuspected only after Compute SetUnhealthy succeeds,
// so soperator must not turn that condition into its own maintenance: no node replacement drain
// overriding the health check reason and no SoperatorChecksNodeMaintenance that deletes the node.
func TestSlurmNodesController_Reconcile_downDrainedWorkerStaysOutOfMaintenance(t *testing.T) {
	ctx := context.Background()
	controller, k8sClient, slurmClusterName, k8sNode, slurmNode := newSlurmNodesControllerForUnhealthyTest(
		t, ctx, slurmapi.Node{States: map[api.V0044NodeState]struct{}{
			api.V0044NodeStateDOWN:  {},
			api.V0044NodeStateDRAIN: {},
		}},
	)
	slurmNode.Reason.Reason = slurmNode.Reason.OriginalReason
	// findDegradedNodes rewrites the listed reason in place, so each listing gets its own copy.
	secondListing := slurmNode
	secondListing.Reason = ptr.To(*slurmNode.Reason)
	apiClient, found := controller.slurmAPIClients.GetClient(slurmClusterName)
	require.True(t, found)
	slurmClient := apiClient.(*slurmapifake.MockClient)
	slurmClient.On("ListNodes", ctx).Return([]slurmapi.Node{slurmNode}, nil).Once()
	slurmClient.On("ListNodes", ctx).Return([]slurmapi.Node{secondListing}, nil).Once()

	req := ctrl.Request{NamespacedName: slurmClusterName}
	_, err := controller.Reconcile(ctx, req)
	require.NoError(t, err)
	require.True(t, hasHardwareIssuesSuspected(t, ctx, k8sClient, k8sNode.Name))

	_, err = controller.Reconcile(ctx, req)
	require.NoError(t, err)

	var updated corev1.Node
	require.NoError(t, k8sClient.Get(ctx, ctrlclient.ObjectKey{Name: k8sNode.Name}, &updated))
	require.True(t, hasHardwareIssuesSuspected(t, ctx, k8sClient, k8sNode.Name))
	require.False(t, slices.ContainsFunc(updated.Status.Conditions, func(condition corev1.NodeCondition) bool {
		return condition.Type == consts.SoperatorChecksK8SNodeMaintenance
	}), "hardware issues must not enter soperator maintenance")
	slurmClient.AssertNotCalled(t, "SlurmV0044PostNodeWithResponse", mock.Anything, mock.Anything, mock.Anything)
}

func TestHardwareIssuesMessage(t *testing.T) {
	tests := []struct {
		name string
		node slurmapi.Node
		want string
	}{
		{
			name: "drain reason and comment",
			node: slurmapi.Node{
				Comment: "GPU 3: Xid 79",
				Reason:  ptr.To(slurmapi.NodeReason{Reason: "[node_problem]", OriginalReason: "[node_problem] gpu_health_check: failed [hc_program]"}),
			},
			want: "[node_problem] gpu_health_check: failed [hc_program]; GPU 3: Xid 79",
		},
		{
			name: "drain reason without comment",
			node: slurmapi.Node{Reason: ptr.To(slurmapi.NodeReason{OriginalReason: "[node_problem] gpu_health_check: failed"})},
			want: "[node_problem] gpu_health_check: failed",
		},
		{
			name: "well-known reason when the original is missing",
			node: slurmapi.Node{Reason: ptr.To(slurmapi.NodeReason{Reason: "[hardware_problem]"})},
			want: "[hardware_problem]",
		},
		{
			name: "comment only",
			node: slurmapi.Node{Comment: "gpu health check failed", Reason: ptr.To(slurmapi.NodeReason{})},
			want: "gpu health check failed",
		},
		{
			name: "nothing known falls back to the generic message",
			node: slurmapi.Node{Comment: "  "},
			want: string(consts.MessageHardwareIssuesSuspected),
		},
		{
			name: "long reason is capped",
			node: slurmapi.Node{Reason: ptr.To(slurmapi.NodeReason{OriginalReason: strings.Repeat("x", MaxMessageLength+10)})},
			want: strings.Repeat("x", MaxMessageLength),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, string(hardwareIssuesMessage(tt.node)))
		})
	}
}

func TestSlurmNodesController_processMaintenance(t *testing.T) {
	tests := []struct {
		name        string
		conditions  []corev1.NodeCondition
		expectDrain bool
	}{
		{
			name:        "no conditions undrains",
			expectDrain: false,
		},
		{
			name: "maintenance scheduled drains",
			conditions: []corev1.NodeCondition{
				{Type: consts.DefaultMaintenanceConditionType, Status: corev1.ConditionTrue},
			},
			expectDrain: true,
		},
		{
			name: "maintenance finished undrains",
			conditions: []corev1.NodeCondition{
				{Type: consts.DefaultMaintenanceConditionType, Status: corev1.ConditionFalse},
			},
			expectDrain: false,
		},
		{
			name: "hardware issues alone are not maintenance",
			conditions: []corev1.NodeCondition{
				{Type: consts.HardwareIssuesSuspected, Status: corev1.ConditionTrue},
			},
			expectDrain: false,
		},
		{
			name: "hardware issues with maintenance scheduled drains",
			conditions: []corev1.NodeCondition{
				{Type: consts.HardwareIssuesSuspected, Status: corev1.ConditionTrue},
				{Type: consts.DefaultMaintenanceConditionType, Status: corev1.ConditionTrue},
			},
			expectDrain: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &SlurmNodesController{MaintenanceConditionType: consts.DefaultMaintenanceConditionType}
			k8sNode := &corev1.Node{Status: corev1.NodeStatus{Conditions: tt.conditions}}

			var drained, undrained bool
			err := controller.processMaintenance(
				context.Background(),
				k8sNode,
				func() error { drained = true; return nil },
				func() error { undrained = true; return nil },
			)
			require.NoError(t, err)
			require.Equal(t, tt.expectDrain, drained, "drainFn called")
			require.Equal(t, !tt.expectDrain, undrained, "undrainFn called")
		})
	}
}

func TestSlurmNodesController_processHealthCheckFailed_waitsForFullyDrainedSlurmNode(t *testing.T) {
	ctx := context.Background()
	controller, k8sClient, slurmClusterName, k8sNode, slurmNode := newSlurmNodesControllerForUnhealthyTest(
		t,
		ctx,
		slurmapi.Node{States: map[api.V0044NodeState]struct{}{
			api.V0044NodeStateALLOCATED: {},
			api.V0044NodeStateDRAIN:     {},
		}},
	)

	err := controller.processHealthCheckFailed(ctx, k8sNode, slurmClusterName, slurmNode, slurmNode.Reason)
	require.NoError(t, err)
	require.False(t, hasHardwareIssuesSuspected(t, ctx, k8sClient, k8sNode.Name))
}

func TestSlurmNodesController_processSetUnhealthy_reassignedInstanceUndrains(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	slurmClusterName := types.NamespacedName{Namespace: "test-ns", Name: "test-cluster"}
	drainTime := time.Date(2026, time.April, 7, 10, 0, 0, 0, time.UTC)
	assignmentTime := drainTime.Add(time.Minute)

	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "instance-new"},
	}
	workerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: slurmClusterName.Namespace,
			Name:      "worker-0",
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:               corev1.PodScheduled,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.NewTime(assignmentTime),
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(k8sNode).
		Build()
	apiReader := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(workerPod).
		Build()

	apiClient := slurmapifake.NewMockClient(t)
	apiClient.On(
		"SlurmV0044PostNodeWithResponse",
		ctx,
		"worker-0",
		mock.MatchedBy(func(body api.SlurmV0044PostNodeJSONRequestBody) bool {
			return body.State != nil &&
				len(*body.State) == 1 &&
				(*body.State)[0] == api.V0044UpdateNodeMsgStateUNDRAIN &&
				body.Comment == nil
		}),
	).Return(&api.SlurmV0044PostNodeResponse{
		JSON200: &api.V0044OpenapiResp{
			Errors: &[]api.V0044OpenapiError{},
		},
	}, nil).Once()

	slurmAPIClients := slurmapi.NewClientSet(context.Background())
	slurmAPIClients.AddClient(slurmClusterName, apiClient)

	controller := NewSlurmNodesController(
		client,
		scheme,
		record.NewFakeRecorder(10),
		slurmAPIClients,
		time.Minute,
		true,
		apiReader,
		"",
	)

	err := controller.processSetUnhealthy(ctx, k8sNode, slurmClusterName, slurmapi.Node{
		Name:       "worker-0",
		InstanceID: k8sNode.Name,
		Comment:    "stale hardware issue comment",
		Reason: ptr.To(slurmapi.NodeReason{
			ChangedAt: drainTime,
		}),
	})
	require.NoError(t, err)

	var updatedNode corev1.Node
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: k8sNode.Name}, &updatedNode))
	require.Empty(t, updatedNode.Status.Conditions)
}

func TestSlurmNodesController_processSetUnhealthy_setsHardwareConditionWhenAssignmentPredatesDrain(t *testing.T) {
	ctx := context.Background()
	controller, k8sClient, slurmClusterName, k8sNode, slurmNode := newSlurmNodesControllerForUnhealthyTest(
		t,
		ctx,
		slurmapi.Node{States: map[api.V0044NodeState]struct{}{
			api.V0044NodeStateIDLE:  {},
			api.V0044NodeStateDRAIN: {},
		}},
	)

	err := controller.processSetUnhealthy(ctx, k8sNode, slurmClusterName, slurmNode)
	require.NoError(t, err)

	var updatedNode corev1.Node
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: k8sNode.Name}, &updatedNode))
	require.Len(t, updatedNode.Status.Conditions, 1)
	require.Equal(t, consts.HardwareIssuesSuspected, updatedNode.Status.Conditions[0].Type)
	require.Equal(t, corev1.ConditionTrue, updatedNode.Status.Conditions[0].Status)
	require.Equal(t, string(consts.ReasonGPUHealthCheckFailed), updatedNode.Status.Conditions[0].Reason)
	require.Equal(t, "[node_problem] gpu health check failed: failed; gpu health check failed", updatedNode.Status.Conditions[0].Message)
}

func TestSlurmNodesController_processSetUnhealthy_missingWorkerPodFallsBackToSetUnhealthy(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	slurmClusterName := types.NamespacedName{Namespace: "test-ns", Name: "test-cluster"}
	drainTime := time.Date(2026, time.April, 7, 10, 0, 0, 0, time.UTC)

	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "instance-old",
			CreationTimestamp: metav1.NewTime(drainTime.Add(-time.Minute)),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(k8sNode).
		WithStatusSubresource(k8sNode).
		WithIndex(&corev1.Pod{}, "spec.nodeName", func(obj ctrlclient.Object) []string {
			pod := obj.(*corev1.Pod)
			return []string{pod.Spec.NodeName}
		}).
		Build()
	apiReader := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	controller := NewSlurmNodesController(
		client,
		scheme,
		record.NewFakeRecorder(10),
		slurmapi.NewClientSet(context.Background()),
		time.Minute,
		true,
		apiReader,
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

	var updatedNode corev1.Node
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: k8sNode.Name}, &updatedNode))
	require.Len(t, updatedNode.Status.Conditions, 1)
	require.Equal(t, consts.HardwareIssuesSuspected, updatedNode.Status.Conditions[0].Type)
}

func TestSlurmNodesController_processSetUnhealthy_missingWorkerPodUndrainsWhenCurrentNodeWasCreatedAfterDrain(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	slurmClusterName := types.NamespacedName{Namespace: "test-ns", Name: "test-cluster"}
	drainTime := time.Date(2026, time.April, 7, 10, 0, 0, 0, time.UTC)

	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "instance-new",
			CreationTimestamp: metav1.NewTime(drainTime.Add(time.Minute)),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(k8sNode).
		Build()
	apiReader := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	apiClient := slurmapifake.NewMockClient(t)
	apiClient.On(
		"SlurmV0044PostNodeWithResponse",
		ctx,
		"worker-0",
		mock.MatchedBy(func(body api.SlurmV0044PostNodeJSONRequestBody) bool {
			return body.State != nil &&
				len(*body.State) == 1 &&
				(*body.State)[0] == api.V0044UpdateNodeMsgStateUNDRAIN &&
				body.Comment == nil
		}),
	).Return(&api.SlurmV0044PostNodeResponse{
		JSON200: &api.V0044OpenapiResp{
			Errors: &[]api.V0044OpenapiError{},
		},
	}, nil).Once()

	slurmAPIClients := slurmapi.NewClientSet(context.Background())
	slurmAPIClients.AddClient(slurmClusterName, apiClient)

	controller := NewSlurmNodesController(
		client,
		scheme,
		record.NewFakeRecorder(10),
		slurmAPIClients,
		time.Minute,
		true,
		apiReader,
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

	var updatedNode corev1.Node
	require.NoError(t, client.Get(ctx, types.NamespacedName{Name: k8sNode.Name}, &updatedNode))
	require.Empty(t, updatedNode.Status.Conditions)
}

func newSlurmNodesControllerForUnhealthyTest(
	t *testing.T,
	ctx context.Context,
	slurmNodes ...slurmapi.Node,
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
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:               corev1.PodScheduled,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.NewTime(drainTime.Add(-time.Minute)),
				},
			},
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
	for i := range slurmNodes {
		node := &slurmNodes[i]
		node.Name = fmt.Sprintf("worker-%d", i)
		node.InstanceID = k8sNode.Name
		node.Comment = "gpu health check failed"
		node.Reason = ptr.To(slurmapi.NodeReason{
			OriginalReason: "[node_problem] gpu health check failed: failed",
			ChangedAt:      drainTime,
		})
		if i > 0 {
			pod := workerPod.DeepCopy()
			pod.Name = node.Name
			pod.ResourceVersion = ""
			require.NoError(t, k8sClient.Create(ctx, pod))
		}
		apiClient.On("GetNode", ctx, node.Name).Return(*node, nil).Once()
	}

	slurmAPIClients := slurmapi.NewClientSet(ctx)
	slurmAPIClients.AddClient(slurmClusterName, apiClient)

	controller := NewSlurmNodesController(
		k8sClient,
		scheme,
		record.NewFakeRecorder(10),
		slurmAPIClients,
		time.Minute,
		true,
		k8sClient,
		"",
	)

	return controller, k8sClient, slurmClusterName, k8sNode, slurmNodes[0]
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

func TestSlurmNodesController_processK8SNodesMaintenance_readsNodesFromCache(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	slurmClusterName := types.NamespacedName{Namespace: "test-ns", Name: "test-cluster"}

	newNode := func(name string, conditions ...corev1.NodeCondition) *corev1.Node {
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status:     corev1.NodeStatus{Conditions: conditions},
		}
	}
	newWorkerPod := func(name, nodeName string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: slurmClusterName.Namespace,
				Name:      name,
				Labels: map[string]string{
					consts.LabelWorkerKey:   consts.LabelWorkerValue,
					consts.LabelInstanceKey: slurmClusterName.Name,
				},
			},
			Spec: corev1.PodSpec{NodeName: nodeName},
		}
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			newNode("instance-maintenance", corev1.NodeCondition{
				Type:   consts.DefaultMaintenanceConditionType,
				Status: corev1.ConditionTrue,
			}),
			newNode("instance-healthy"),
			newWorkerPod("worker-0", "instance-maintenance"),
			newWorkerPod("worker-1", "instance-healthy"),
		).
		WithIndex(&corev1.Pod{}, "spec.nodeName", func(obj ctrlclient.Object) []string {
			return []string{obj.(*corev1.Pod).Spec.NodeName}
		}).
		Build()

	apiClient := slurmapifake.NewMockClient(t)
	apiClient.On(
		"SlurmV0044PostNodeWithResponse",
		ctx,
		"worker-0",
		mock.MatchedBy(func(body api.SlurmV0044PostNodeJSONRequestBody) bool {
			return body.State != nil &&
				len(*body.State) == 1 &&
				(*body.State)[0] == api.V0044UpdateNodeMsgStateDRAIN
		}),
	).Return(&api.SlurmV0044PostNodeResponse{
		JSON200: &api.V0044OpenapiResp{Errors: &[]api.V0044OpenapiError{}},
	}, nil).Once()
	apiClient.On("GetNode", ctx, "worker-0").Return(slurmapi.Node{
		Name:   "worker-0",
		States: map[api.V0044NodeState]struct{}{api.V0044NodeStateIDLE: {}, api.V0044NodeStateDRAIN: {}},
	}, nil).Once()

	slurmAPIClients := slurmapi.NewClientSet(ctx)
	slurmAPIClients.AddClient(slurmClusterName, apiClient)

	controller := NewSlurmNodesController(
		k8sClient,
		scheme,
		record.NewFakeRecorder(10),
		slurmAPIClients,
		time.Minute,
		true,
		// Nodes must come from the cached client, so an empty uncached reader is enough here.
		fake.NewClientBuilder().WithScheme(scheme).Build(),
		"",
	)

	require.NoError(t, controller.processK8SNodesMaintenance(ctx))

	var maintained corev1.Node
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: "instance-maintenance"}, &maintained))
	require.True(t, slices.ContainsFunc(maintained.Status.Conditions, func(c corev1.NodeCondition) bool {
		return c.Type == consts.SoperatorChecksK8SNodeMaintenance && c.Status == corev1.ConditionTrue
	}))

	var healthy corev1.Node
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: "instance-healthy"}, &healthy))
	require.Empty(t, healthy.Status.Conditions)
}
