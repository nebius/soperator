package checkcontroller_test

import (
	"context"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	. "nebius.ai/slurm-operator/internal/checkcontroller"
	reconciler "nebius.ai/slurm-operator/internal/controller/reconciler"
)

func TestMarkNodeUnschedulable(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = slurmv1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	objs := []runtime.Object{}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
	r := &CheckControllerReconciler{
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

	err = r.MarkNodeUnschedulable(ctx, node)
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

func TestShouldSkipEviction(t *testing.T) {
	tests := []struct {
		name     string
		priority *int32
		expected bool
	}{
		{name: "Low priority pod", priority: int32Ptr(500), expected: false},
		{name: "High priority pod", priority: int32Ptr(10000000), expected: true},
		{name: "No priority pod", priority: nil, expected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pod := corev1.Pod{
				Spec: corev1.PodSpec{
					Priority: test.priority,
				},
			}

			result := ShouldSkipEviction(pod)
			if result != test.expected {
				t.Errorf("expected %v, got %v", test.expected, result)
			}
		})
	}
}

func int32Ptr(i int32) *int32 {
	return &i
}

func TestEvictPod(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = slurmv1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	objs := []runtime.Object{}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
	r := &CheckControllerReconciler{
		Reconciler: &reconciler.Reconciler{
			Client: fakeClient,
		},
	}

	ctx := context.TODO()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	err := fakeClient.Create(ctx, pod)
	if err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}

	err = r.EvictPod(ctx, pod)
	if err != nil {
		t.Errorf("evictPod returned an error: %v", err)
	}
}

func TestSetNodeCondition(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	objs := []runtime.Object{}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
	r := &CheckControllerReconciler{
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

	err = r.SetNodeConditionIfNotExists(ctx, node, SlurmNodeCondition, corev1.ConditionTrue, "TestReason")
	if err != nil {
		t.Errorf("setNodeCondition returned an error: %v", err)
	}

	updatedNode := &corev1.Node{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-node"}, updatedNode)
	if err != nil {
		t.Fatalf("failed to get updated node: %v", err)
	}

	fmt.Println(updatedNode.Status.Conditions)
	if len(updatedNode.Status.Conditions) == 0 {
		t.Errorf("node condition was not set")
	}
}
