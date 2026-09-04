package controllerconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTrimNodeForCache(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:          "worker-0",
			Labels:        map[string]string{"topology.nebius.com/tier-1": "switch-1"},
			ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "kubelet"}},
		},
		Spec: corev1.NodeSpec{Unschedulable: true},
		Status: corev1.NodeStatus{
			Images:     []corev1.ContainerImage{{Names: []string{"cr.eu-north1.nebius.cloud/soperator:1.0.0"}}},
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	}

	trimmed, err := TrimNodeForCache(node)
	require.NoError(t, err)

	trimmedNode, ok := trimmed.(*corev1.Node)
	require.True(t, ok)
	require.Empty(t, trimmedNode.ManagedFields)
	require.Empty(t, trimmedNode.Status.Images)

	// Everything the controllers actually read must survive.
	require.Equal(t, "worker-0", trimmedNode.Name)
	require.Equal(t, "switch-1", trimmedNode.Labels["topology.nebius.com/tier-1"])
	require.True(t, trimmedNode.Spec.Unschedulable)
	require.Equal(t, corev1.NodeReady, trimmedNode.Status.Conditions[0].Type)
}

func TestTrimNodeForCache_passesThroughNonNodes(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker-0"}}

	trimmed, err := TrimNodeForCache(pod)
	require.NoError(t, err)
	require.Same(t, pod, trimmed)
}
