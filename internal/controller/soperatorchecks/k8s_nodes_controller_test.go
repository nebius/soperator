package soperatorchecks

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"nebius.ai/slurm-operator/internal/consts"
)

func TestK8SNodesController_processNotReadyCondition(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name           string
		node           *corev1.Node
		expectError    bool
		expectDeletion bool
	}{
		{
			name: "node is ready - no action",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expectError:    false,
			expectDeletion: false,
		},
		{
			name: "no ready condition - no action",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{},
				},
			},
			expectError:    false,
			expectDeletion: false,
		},
		{
			name: "node not ready less than 15 minutes - no action",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeReady,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-10 * time.Minute)},
						},
					},
				},
			},
			expectError:    false,
			expectDeletion: false,
		},
		{
			name: "node not ready more than 15 minutes - delete node",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeReady,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-16 * time.Minute)},
						},
					},
				},
			},
			expectError:    false,
			expectDeletion: true,
		},
		{
			name: "node not ready more than 15 minutes with drain condition - undrain and delete",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeReady,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-16 * time.Minute)},
						},
						{
							Type:   consts.SlurmNodeDrain,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expectError:    false,
			expectDeletion: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.node).
				Build()

			recorder := record.NewFakeRecorder(10)
			controller := NewK8SNodesController(client, scheme, recorder, 15*time.Minute, true, consts.DefaultMaintenanceConditionType, "")

			ctx := context.Background()
			err := controller.processNotReadyCondition(ctx, tt.node)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Check if node was deleted
			var node corev1.Node
			getErr := client.Get(ctx, types.NamespacedName{Name: tt.node.Name}, &node)
			if tt.expectDeletion {
				assert.Error(t, getErr, "Node should be deleted")
			} else {
				assert.NoError(t, getErr, "Node should not be deleted")
			}
		})
	}
}

func TestK8SNodesController_requeueDurationForNotReady(t *testing.T) {
	tests := []struct {
		name              string
		node              *corev1.Node
		expectRequeue     bool
		expectMinDuration time.Duration
		expectMaxDuration time.Duration
	}{
		{
			name: "node is ready - no requeue",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expectRequeue: false,
		},
		{
			name: "no ready condition - no requeue",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{},
				},
			},
			expectRequeue: false,
		},
		{
			name: "node not ready for 1 minute - requeue in ~14 minutes",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeReady,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-1 * time.Minute)},
						},
					},
				},
			},
			expectRequeue:     true,
			expectMinDuration: 14*time.Minute + 20*time.Second, // ~14 minutes + buffer - some tolerance
			expectMaxDuration: 14*time.Minute + 40*time.Second, // ~14 minutes + buffer + some tolerance
		},
		{
			name: "node not ready for 16 minutes - no requeue",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeReady,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-16 * time.Minute)},
						},
					},
				},
			},
			expectRequeue: false,
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)
	controller := NewK8SNodesController(client, scheme, recorder, 15*time.Minute, false, consts.DefaultMaintenanceConditionType, "")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration := controller.requeueDurationForNotReady(tt.node)

			if tt.expectRequeue {
				assert.Greater(t, duration, time.Duration(0), "Should return positive duration for requeue")
				assert.GreaterOrEqual(t, duration, tt.expectMinDuration, "Duration should be at least minimum expected")
				assert.LessOrEqual(t, duration, tt.expectMaxDuration, "Duration should not exceed maximum expected")
			} else {
				assert.Equal(t, time.Duration(0), duration, "Should not requeue")
			}
		})
	}
}

func TestK8SNodesController_Reconcile_NotReadyFlow(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name               string
		node               *corev1.Node
		expectRequeue      bool
		expectRequeueAfter bool
		expectNodeDeletion bool
	}{
		{
			name: "ready node - no requeue",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expectRequeue:      false,
			expectRequeueAfter: false,
			expectNodeDeletion: false,
		},
		{
			name: "not ready for 2 minutes - requeue after delay",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeReady,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-2 * time.Minute)},
						},
					},
				},
			},
			expectRequeue:      true,
			expectRequeueAfter: true,
			expectNodeDeletion: false,
		},
		{
			name: "not ready for 16 minutes - delete node, no requeue",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:               corev1.NodeReady,
							Status:             corev1.ConditionFalse,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-16 * time.Minute)},
						},
					},
				},
			},
			expectRequeue:      false,
			expectRequeueAfter: false,
			expectNodeDeletion: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.node).
				Build()

			recorder := record.NewFakeRecorder(10)
			controller := NewK8SNodesController(client, scheme, recorder, 15*time.Minute, true, consts.DefaultMaintenanceConditionType, "")

			ctx := context.Background()
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: tt.node.Name,
				},
			}

			result, err := controller.Reconcile(ctx, req)
			assert.NoError(t, err)

			if tt.expectRequeue && tt.expectRequeueAfter {
				assert.True(t, result.Requeue || result.RequeueAfter > 0, "Should requeue")
				if result.RequeueAfter > 0 {
					assert.Greater(t, result.RequeueAfter, time.Duration(0), "RequeueAfter should be positive")
				}
			} else {
				assert.False(t, result.Requeue, "Should not requeue")
				assert.Equal(t, time.Duration(0), result.RequeueAfter, "RequeueAfter should be zero")
			}

			// Check if node was deleted
			var node corev1.Node
			getErr := client.Get(ctx, types.NamespacedName{Name: tt.node.Name}, &node)
			if tt.expectNodeDeletion {
				assert.Error(t, getErr, "Node should be deleted")
			} else {
				assert.NoError(t, getErr, "Node should not be deleted")
			}
		})
	}
}
