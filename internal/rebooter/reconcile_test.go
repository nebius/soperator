package rebooter_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
			result := r.IsNodeTaintedWithNoExecute(tt.node)
			if result != tt.expected {
				t.Errorf("IsNodeTaintedWithNoExecute() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func newRebooterWithFakeClient(scheme *runtime.Scheme, objs ...client.Object) *RebooterReconciler {
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithIndex(&corev1.Pod{}, "spec.nodeName", func(obj client.Object) []string {
			pod := obj.(*corev1.Pod)
			if pod.Spec.NodeName != "" {
				return []string{pod.Spec.NodeName}
			}
			return nil
		}).
		Build()
	return &RebooterReconciler{
		Reconciler: &reconciler.Reconciler{
			Client: fakeClient,
		},
		APIReader: fakeClient,
	}
}

func TestAreAllPodsEvicted(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	const nodeName = "test-node"

	daemonSetOwner := []metav1.OwnerReference{{Kind: "DaemonSet", Name: "ds", APIVersion: "apps/v1"}}
	noExecuteToleration := corev1.Toleration{Effect: corev1.TaintEffectNoExecute}
	existsToleration := corev1.Toleration{Operator: corev1.TolerationOpExists}

	tests := []struct {
		name    string
		pods    []corev1.Pod
		wantErr bool
	}{
		{
			name:    "no pods — node is drained",
			pods:    nil,
			wantErr: false,
		},
		{
			name: "only DaemonSet pods — exempt",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ds-pod", Namespace: "default", OwnerReferences: daemonSetOwner},
					Spec:       corev1.PodSpec{NodeName: nodeName},
				},
			},
			wantErr: false,
		},
		{
			name: "pod with NoExecute toleration — exempt",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "tolerated-pod", Namespace: "default"},
					Spec:       corev1.PodSpec{NodeName: nodeName, Tolerations: []corev1.Toleration{noExecuteToleration}},
				},
			},
			wantErr: false,
		},
		{
			name: "pod with Exists toleration — exempt",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "exists-tolerated-pod", Namespace: "default"},
					Spec:       corev1.PodSpec{NodeName: nodeName, Tolerations: []corev1.Toleration{existsToleration}},
				},
			},
			wantErr: false,
		},
		{
			name: "blocking pod — not evicted",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "blocking-pod", Namespace: "default"},
					Spec:       corev1.PodSpec{NodeName: nodeName},
				},
			},
			wantErr: true,
		},
		{
			name: "mix of exempt and blocking pods — returns error for blocking",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ds-pod", Namespace: "default", OwnerReferences: daemonSetOwner},
					Spec:       corev1.PodSpec{NodeName: nodeName},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "blocking-pod", Namespace: "default"},
					Spec:       corev1.PodSpec{NodeName: nodeName},
				},
			},
			wantErr: true,
		},
		{
			name: "pod on different node — not counted",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "other-node-pod", Namespace: "default"},
					Spec:       corev1.PodSpec{NodeName: "other-node"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := make([]client.Object, len(tt.pods))
			for i := range tt.pods {
				objs[i] = &tt.pods[i]
			}
			r := newRebooterWithFakeClient(scheme, objs...)

			err := r.AreAllPodsEvicted(context.Background(), nodeName)
			if tt.wantErr {
				require.Error(t, err)
				var podErr *PodNotEvictableError
				assert.ErrorAs(t, err, &podErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestAreAllPodsEvictedCacheThreshold verifies that a blocking pod is still
// detected when the total pod count exceeds podEvictionCacheThreshold and the
// paginated API-reader path is taken.  The fake client used as APIReader does
// not support real server-side pagination, but it does return all objects, so
// the correctness of the filtering logic is exercised end-to-end.
func TestAreAllPodsEvictedCacheThreshold(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	const nodeName = "big-node"
	// Build just over the threshold (1001 pods) to force the paginated path.
	const podCount = 1001

	objs := make([]client.Object, podCount)
	for i := range podCount {
		objs[i] = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%d", i),
				Namespace: "default",
			},
			Spec: corev1.PodSpec{NodeName: nodeName},
		}
	}

	r := newRebooterWithFakeClient(scheme, objs...)

	err := r.AreAllPodsEvicted(context.Background(), nodeName)
	require.Error(t, err)
	var podErr *PodNotEvictableError
	assert.ErrorAs(t, err, &podErr, "expected PodNotEvictableError for blocking pod on large node")
}

func TestHasTolerationForExists(t *testing.T) {
	tests := []struct {
		name     string
		pod      corev1.Pod
		expected bool
	}{
		{
			name: "Pod with Exists toleration",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{
						{
							Operator: corev1.TolerationOpExists,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Pod without Exists toleration",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{
						{
							Operator: corev1.TolerationOpEqual,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Pod with multiple tolerations including Exists",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{
						{
							Operator: corev1.TolerationOpEqual,
						},
						{
							Operator: corev1.TolerationOpExists,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Pod with no tolerations",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasTolerationForExists(tt.pod)
			if result != tt.expected {
				t.Errorf("HasTolerationForExists() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
