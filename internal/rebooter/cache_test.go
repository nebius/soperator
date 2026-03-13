package rebooter_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "nebius.ai/slurm-operator/internal/rebooter"
)

func TestPodCacheTransform(t *testing.T) {
	fullPod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "DaemonSet", Name: "ds"},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Tolerations: []corev1.Toleration{
				{Effect: corev1.TaintEffectNoExecute},
			},
			Containers: []corev1.Container{
				{Name: "main", Image: "busybox"},
			},
			Volumes: []corev1.Volume{
				{Name: "data"},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	t.Run("returns stripped Pod with required fields only", func(t *testing.T) {
		result, err := PodCacheTransform(fullPod)
		require.NoError(t, err)

		got, ok := result.(*corev1.Pod)
		require.True(t, ok, "result must be *corev1.Pod")

		// TypeMeta preserved as-is.
		assert.Equal(t, fullPod.TypeMeta, got.TypeMeta)

		// Only minimal identity fields are kept; large ObjectMeta fields are dropped.
		assert.Equal(t, fullPod.Name, got.Name)
		assert.Equal(t, fullPod.Namespace, got.Namespace)
		assert.Equal(t, fullPod.UID, got.UID)
		assert.Equal(t, fullPod.OwnerReferences, got.OwnerReferences)
		assert.Empty(t, got.Annotations, "Annotations must be stripped")
		assert.Empty(t, got.Labels, "Labels must be stripped")
		assert.Empty(t, got.ManagedFields, "ManagedFields must be stripped")

		// Required Spec fields preserved; rest dropped.
		assert.Equal(t, fullPod.Spec.NodeName, got.Spec.NodeName)
		assert.Equal(t, fullPod.Spec.Tolerations, got.Spec.Tolerations)
		assert.Empty(t, got.Spec.Containers, "Containers must be stripped")
		assert.Empty(t, got.Spec.Volumes, "Volumes must be stripped")

		// Status fully dropped.
		assert.Empty(t, got.Status.Phase, "Status must be stripped")

		// Slices are independent copies — mutating the original must not affect the cached pod.
		fullPod.OwnerReferences[0].Name = "mutated"
		assert.NotEqual(t, "mutated", got.OwnerReferences[0].Name, "OwnerReferences must be a copy")
		fullPod.Spec.Tolerations[0].Effect = corev1.TaintEffectNoSchedule
		assert.NotEqual(t, corev1.TaintEffectNoSchedule, got.Spec.Tolerations[0].Effect, "Tolerations must be a copy")
	})

	t.Run("passes through non-Pod objects unchanged", func(t *testing.T) {
		node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}
		result, err := PodCacheTransform(node)
		require.NoError(t, err)
		assert.Equal(t, node, result, "non-Pod input must be returned as-is")
	})
}
