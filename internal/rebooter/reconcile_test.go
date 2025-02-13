package rebooter_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

			result := r.CheckNodeCondition(ctx, &tc.node.Status.Conditions[0], tc.typeCondition, tc.statusCondition)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSetNodeSchedulable(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &RebooterReconciler{
		Reconciler: &reconciler.Reconciler{
			Client: fakeClient,
		},
	}

	ctx := context.TODO()
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}

	err := fakeClient.Create(ctx, node)
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	err = r.SetNodeUnschedulable(ctx, node, true)
	if err != nil {
		t.Errorf("markNodeUnschedulable returned an error: %v", err)
	}

	updatedNode := &corev1.Node{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-node"}, updatedNode)
	if err != nil {
		t.Fatalf("failed to get updated node: %v", err)
	}

	if !updatedNode.Spec.Unschedulable {
		t.Errorf("node was not marked as unschedulable")
	}
}

func TestSetNodeConditionIfNotExists(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &RebooterReconciler{
		Reconciler: &reconciler.Reconciler{
			Client: fakeClient,
		},
	}

	ctx := context.TODO()
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}

	err := fakeClient.Create(ctx, node)
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	err = r.SetNodeConditionIfNotExists(ctx, node, consts.SlurmNodeDrain, corev1.ConditionTrue, consts.ReasonNodeDrained, consts.MessageDrained)
	if err != nil {
		t.Errorf("setNodeCondition returned an error: %v", err)
	}

	updatedNode := &corev1.Node{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-node"}, updatedNode)
	if err != nil {
		t.Fatalf("failed to get updated node: %v", err)
	}

	if len(updatedNode.Status.Conditions) == 0 {
		t.Errorf("node condition was not set")
	}
	if updatedNode.Status.Conditions[0].Type != consts.SlurmNodeDrain {
		t.Errorf("node condition type is not correct")
	}
	if updatedNode.Status.Conditions[0].Status != corev1.ConditionTrue {
		t.Errorf("node condition status is not correct")
	}
	if updatedNode.Status.Conditions[0].Reason != string(consts.ReasonNodeDrained) {
		t.Errorf("node condition reason is not correct")
	}
}

func TestTaintNodeWithNoExecute(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &RebooterReconciler{
		Reconciler: &reconciler.Reconciler{
			Client: fakeClient,
		},
	}

	ctx := context.TODO()
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}

	err := fakeClient.Create(ctx, node)
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	// Test adding the taint
	err = r.TaintNodeWithNoExecute(ctx, node, true)
	if err != nil {
		t.Errorf("TaintNodeWithNoExecute returned an error: %v", err)
	}

	updatedNode := &corev1.Node{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-node"}, updatedNode)
	if err != nil {
		t.Fatalf("failed to get updated node: %v", err)
	}

	if len(updatedNode.Spec.Taints) == 0 {
		t.Errorf("node was not tainted")
	}
	if updatedNode.Spec.Taints[0].Effect != corev1.TaintEffectNoExecute {
		t.Errorf("taint effect is not correct")
	}

	// Test removing the taint
	err = r.TaintNodeWithNoExecute(ctx, node, false)
	if err != nil {
		t.Errorf("TaintNodeWithNoExecute returned an error: %v", err)
	}

	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-node"}, updatedNode)
	if err != nil {
		t.Fatalf("failed to get updated node: %v", err)
	}

	if len(updatedNode.Spec.Taints) != 0 {
		t.Errorf("node was not untainted")
	}
}

func TestIsNodeTaintedWithNoExecute(t *testing.T) {
	tests := []struct {
		name     string
		node     *corev1.Node
		expected bool
	}{
		{
			name: "Node with NoExecute taint",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{
							Key:    "node.kubernetes.io/NoExecute",
							Value:  "true",
							Effect: corev1.TaintEffectNoExecute,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Node without NoExecute taint",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{
							Key:    "node.kubernetes.io/NoSchedule",
							Value:  "true",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Node with multiple taints including NoExecute",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{
						{
							Key:    "node.kubernetes.io/NoSchedule",
							Value:  "true",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "node.kubernetes.io/NoExecute",
							Value:  "true",
							Effect: corev1.TaintEffectNoExecute,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Node with no taints",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{
					Taints: []corev1.Taint{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RebooterReconciler{}
			result := r.IsNodeTaintedWithNoExecute(context.Background(), tt.node)
			if result != tt.expected {
				t.Errorf("IsNodeTaintedWithNoExecute() = %v, want %v", result, tt.expected)
			}
		})
	}
}
