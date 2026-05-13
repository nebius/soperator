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
		client,
		scheme,
		record.NewFakeRecorder(10),
		slurmAPIClients,
		time.Minute,
		true,
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
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	slurmClusterName := types.NamespacedName{Namespace: "test-ns", Name: "test-cluster"}
	drainTime := time.Date(2026, time.April, 7, 10, 0, 0, 0, time.UTC)
	assignmentTime := drainTime.Add(-time.Minute)

	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "instance-old"},
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
		WithStatusSubresource(k8sNode).
		Build()
	apiReader := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(workerPod).
		Build()

	controller := NewSlurmNodesController(
		client,
		scheme,
		record.NewFakeRecorder(10),
		slurmapi.NewClientSet(),
		time.Minute,
		true,
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
	require.Equal(t, corev1.ConditionTrue, updatedNode.Status.Conditions[0].Status)
	require.Equal(t, string(consts.ReasonGPUHealthCheckFailed), updatedNode.Status.Conditions[0].Reason)
	require.Equal(t, "gpu health check failed", updatedNode.Status.Conditions[0].Message)
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
		Build()
	apiReader := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	controller := NewSlurmNodesController(
		client,
		scheme,
		record.NewFakeRecorder(10),
		slurmapi.NewClientSet(),
		time.Minute,
		true,
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
		client,
		scheme,
		record.NewFakeRecorder(10),
		slurmAPIClients,
		time.Minute,
		true,
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
