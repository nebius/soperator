package updatecontroller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *RollingUpdateReconciler) getK8sNodeCordonStates(ctx context.Context, pods []corev1.Pod) (map[string]bool, error) {
	cordonStates := make(map[string]bool)
	for _, pod := range pods {
		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			continue
		}
		if _, known := cordonStates[nodeName]; known {
			continue
		}
		node := &corev1.Node{}
		if err := r.Get(ctx, types.NamespacedName{Name: nodeName}, node); client.IgnoreNotFound(err) != nil {
			return nil, fmt.Errorf("get worker Kubernetes node %s: %w", nodeName, err)
		}
		cordonStates[nodeName] = node.Spec.Unschedulable
	}
	return cordonStates, nil
}
