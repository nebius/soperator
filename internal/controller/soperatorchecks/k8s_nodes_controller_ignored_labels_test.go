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

func TestK8SNodesController_ProcessDrainCondition_IgnoredLabels(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name              string
		ignoredNodeLabels string
		nodeLabels        map[string]string
		conditions        []corev1.NodeCondition
		expectSkip        bool
		expectCondition   bool
		expectError       bool
	}{
		{
			name:              "no ignored labels - process normally",
			ignoredNodeLabels: "",
			nodeLabels: map[string]string{
				"env": "prod",
			},
			conditions: []corev1.NodeCondition{
				{
					Type:   consts.SoperatorChecksK8SNodeMaintenance,
					Status: corev1.ConditionTrue,
				},
			},
			expectSkip:      false,
			expectCondition: true,
			expectError:     false,
		},
		{
			name:              "node has ignored label - skip processing",
			ignoredNodeLabels: "env=prod",
			nodeLabels: map[string]string{
				"env": "prod",
			},
			conditions: []corev1.NodeCondition{
				{
					Type:   consts.SoperatorChecksK8SNodeMaintenance,
					Status: corev1.ConditionTrue,
				},
			},
			expectSkip:      true,
			expectCondition: false,
			expectError:     false,
		},
		{
			name:              "node has different label value - process normally",
			ignoredNodeLabels: "env=prod",
			nodeLabels: map[string]string{
				"env": "dev",
			},
			conditions: []corev1.NodeCondition{
				{
					Type:   consts.SoperatorChecksK8SNodeMaintenance,
					Status: corev1.ConditionTrue,
				},
			},
			expectSkip:      false,
			expectCondition: true,
			expectError:     false,
		},
		{
			name:              "multiple ignored labels - node matches one",
			ignoredNodeLabels: "env=prod,tier=critical",
			nodeLabels: map[string]string{
				"env":  "dev",
				"tier": "critical",
			},
			conditions: []corev1.NodeCondition{
				{
					Type:   consts.SoperatorChecksK8SNodeMaintenance,
					Status: corev1.ConditionTrue,
				},
			},
			expectSkip:      true,
			expectCondition: false,
			expectError:     false,
		},
		{
			name:              "multiple ignored labels - node matches all",
			ignoredNodeLabels: "env=prod,tier=critical",
			nodeLabels: map[string]string{
				"env":  "prod",
				"tier": "critical",
			},
			conditions: []corev1.NodeCondition{
				{
					Type:   consts.SoperatorChecksK8SNodeMaintenance,
					Status: corev1.ConditionTrue,
				},
			},
			expectSkip:      true,
			expectCondition: false,
			expectError:     false,
		},
		{
			name:              "multiple ignored labels - node matches none",
			ignoredNodeLabels: "env=prod,tier=critical",
			nodeLabels: map[string]string{
				"env":  "dev",
				"tier": "standard",
			},
			conditions: []corev1.NodeCondition{
				{
					Type:   consts.SoperatorChecksK8SNodeMaintenance,
					Status: corev1.ConditionTrue,
				},
			},
			expectSkip:      false,
			expectCondition: true,
			expectError:     false,
		},
		{
			name:              "complex label keys - matching",
			ignoredNodeLabels: "topology.kubernetes.io/zone=us-west-1a",
			nodeLabels: map[string]string{
				"topology.kubernetes.io/zone": "us-west-1a",
			},
			conditions: []corev1.NodeCondition{
				{
					Type:   consts.SoperatorChecksK8SNodeMaintenance,
					Status: corev1.ConditionTrue,
				},
			},
			expectSkip:      true,
			expectCondition: false,
			expectError:     false,
		},
		{
			name:              "node with hardware issues - ignored label",
			ignoredNodeLabels: "env=prod",
			nodeLabels: map[string]string{
				"env": "prod",
			},
			conditions: []corev1.NodeCondition{
				{
					Type:   consts.HardwareIssuesSuspected,
					Status: corev1.ConditionTrue,
				},
			},
			expectSkip:      true,
			expectCondition: false,
			expectError:     false,
		},
		{
			name:              "node already drained - ignored label",
			ignoredNodeLabels: "env=prod",
			nodeLabels: map[string]string{
				"env": "prod",
			},
			conditions: []corev1.NodeCondition{
				{
					Type:   consts.SlurmNodeDrain,
					Status: corev1.ConditionTrue,
					Reason: string(consts.ReasonNodeDrained),
				},
				{
					Type:   consts.SoperatorChecksK8SNodeMaintenance,
					Status: corev1.ConditionTrue,
				},
			},
			expectSkip:      true,
			expectCondition: false,
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: tt.nodeLabels,
				},
				Status: corev1.NodeStatus{
					Conditions: tt.conditions,
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(node).
				Build()

			recorder := record.NewFakeRecorder(100)
			controller := NewK8SNodesController(
				fakeClient,
				scheme,
				recorder,
				15*time.Minute,
				true,
				consts.DefaultMaintenanceConditionType,
				tt.ignoredNodeLabels,
			)

			err := controller.processDrainCondition(context.Background(), node)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify node wasn't modified if it should be skipped
			if tt.expectSkip {
				updatedNode := &corev1.Node{}
				err = fakeClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, updatedNode)
				require.NoError(t, err)

				// Node conditions should remain the same (no SlurmNodeDrain condition added)
				hasDrainCondition := false
				for _, cond := range updatedNode.Status.Conditions {
					if cond.Type == consts.SlurmNodeDrain && cond.Status == corev1.ConditionTrue {
						hasDrainCondition = true
						break
					}
				}

				if tt.expectCondition {
					assert.True(t, hasDrainCondition, "expected drain condition to be set")
				} else {
					// If we started with a drain condition, check it wasn't changed
					originalHadDrain := false
					for _, cond := range tt.conditions {
						if cond.Type == consts.SlurmNodeDrain && cond.Status == corev1.ConditionTrue {
							originalHadDrain = true
							break
						}
					}
					assert.Equal(t, originalHadDrain, hasDrainCondition, "drain condition should not be modified when node is ignored")
				}
			}
		})
	}
}

func TestK8SNodesController_InvalidIgnoredLabels(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name              string
		ignoredNodeLabels string
		shouldLogError    bool
	}{
		{
			name:              "valid labels",
			ignoredNodeLabels: "env=prod,tier=critical",
			shouldLogError:    false,
		},
		{
			name:              "invalid format - missing value",
			ignoredNodeLabels: "env=prod,tier",
			shouldLogError:    true,
		},
		{
			name:              "invalid format - missing equals",
			ignoredNodeLabels: "envprod",
			shouldLogError:    true,
		},
		{
			name:              "empty string",
			ignoredNodeLabels: "",
			shouldLogError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			recorder := record.NewFakeRecorder(100)

			// Controller should be created successfully even with invalid labels
			// (it falls back to empty matcher)
			controller := NewK8SNodesController(
				fakeClient,
				scheme,
				recorder,
				15*time.Minute,
				true,
				consts.DefaultMaintenanceConditionType,
				tt.ignoredNodeLabels,
			)

			require.NotNil(t, controller)
			require.NotNil(t, controller.nodeLabelMatcher)

			// Controller should work normally (with no ignored labels if invalid)
			if tt.shouldLogError {
				// With invalid labels, matcher should have no labels configured
				assert.False(t, controller.nodeLabelMatcher.HasIgnoredLabels())
			}
		})
	}
}

func TestK8SNodesController_Reconcile_WithIgnoredLabels(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name              string
		ignoredNodeLabels string
		nodeLabels        map[string]string
		conditions        []corev1.NodeCondition
		expectProcessing  bool
	}{
		{
			name:              "node with ignored label should not be processed",
			ignoredNodeLabels: "maintenance-exempt=true",
			nodeLabels: map[string]string{
				"maintenance-exempt": "true",
			},
			conditions: []corev1.NodeCondition{
				{
					Type:   consts.SoperatorChecksK8SNodeMaintenance,
					Status: corev1.ConditionTrue,
				},
			},
			expectProcessing: false,
		},
		{
			name:              "node without ignored label should be processed",
			ignoredNodeLabels: "maintenance-exempt=true",
			nodeLabels: map[string]string{
				"env": "prod",
			},
			conditions: []corev1.NodeCondition{
				{
					Type:   consts.SoperatorChecksK8SNodeMaintenance,
					Status: corev1.ConditionTrue,
				},
			},
			expectProcessing: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: tt.nodeLabels,
				},
				Status: corev1.NodeStatus{
					Conditions: tt.conditions,
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(node).
				Build()

			recorder := record.NewFakeRecorder(100)
			controller := NewK8SNodesController(
				fakeClient,
				scheme,
				recorder,
				15*time.Minute,
				true,
				consts.DefaultMaintenanceConditionType,
				tt.ignoredNodeLabels,
			)

			ctx := context.Background()
			result, err := controller.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: node.Name},
			})

			require.NoError(t, err)
			assert.Equal(t, ctrl.Result{}, result)

			// Check if node was processed by examining if drain condition was added
			updatedNode := &corev1.Node{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: node.Name}, updatedNode)
			require.NoError(t, err)

			hasDrainCondition := false
			for _, cond := range updatedNode.Status.Conditions {
				if cond.Type == consts.SlurmNodeDrain && cond.Status == corev1.ConditionTrue {
					hasDrainCondition = true
					break
				}
			}

			if tt.expectProcessing {
				assert.True(t, hasDrainCondition, "expected node to be processed and drain condition to be set")
			} else {
				assert.False(t, hasDrainCondition, "expected node to be skipped and no drain condition to be set")
			}
		})
	}
}
