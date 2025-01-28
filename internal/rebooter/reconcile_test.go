package rebooter_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	. "nebius.ai/slurm-operator/internal/rebooter"
)

func TestCheckNodeCondition(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &RebooterReconciler{
		Reconciler: &reconciler.Reconciler{
			Client: fakeClient,
		},
	}

	ctx := context.TODO()

	testCases := []struct {
		name            string
		node            *corev1.Node
		statusCondition corev1.ConditionStatus
		typeCondition   corev1.NodeConditionType
		expected        bool
	}{
		{
			name: "CheckIfNodeNeedsDrain true",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node-0",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   consts.SlurmNodeDrain,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			typeCondition:   consts.SlurmNodeDrain,
			statusCondition: corev1.ConditionTrue,
			expected:        true,
		},
		{
			name: "CheckIfNodeNeedsDrain false",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node-1",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   consts.SlurmNodeDrain,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			typeCondition:   consts.SlurmNodeDrain,
			statusCondition: corev1.ConditionTrue,
			expected:        false,
		},
		{
			name: "checkIfNodeNeedsReboot true",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node-2",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   consts.SlurmNodeReboot,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			typeCondition:   consts.SlurmNodeReboot,
			statusCondition: corev1.ConditionTrue,
			expected:        true,
		},
		{
			name: "checkIfNodeNeedsReboot false",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node-3",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   consts.SlurmNodeReboot,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			typeCondition:   consts.SlurmNodeReboot,
			statusCondition: corev1.ConditionTrue,
			expected:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := fakeClient.Create(ctx, tc.node)
			assert.NoError(t, err)

			result := r.CheckNodeCondition(ctx, tc.node, tc.typeCondition, tc.statusCondition)
			assert.Equal(t, tc.expected, result)
		})
	}
}
