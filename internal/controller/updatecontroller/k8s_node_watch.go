package updatecontroller

import (
	"context"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const workerPodNodeNameIndex = "soperator.rollingUpdate.nodeName"

func (r *RollingUpdateReconciler) mapNodeToStatefulSetRequests(ctx context.Context, obj client.Object) []ctrl.Request {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.MatchingFields{workerPodNodeNameIndex: obj.GetName()}); err != nil {
		log.FromContext(ctx).Error(err, "List workers on Kubernetes node", "node", obj.GetName())
		return nil
	}
	seen := make(map[types.NamespacedName]struct{})
	var requests []ctrl.Request
	for _, pod := range pods.Items {
		owner := metav1.GetControllerOf(&pod)
		if owner == nil || owner.Kind != "StatefulSet" || owner.APIVersion != kruisev1b1.GroupVersion.String() {
			continue
		}
		key := types.NamespacedName{Namespace: pod.Namespace, Name: owner.Name}
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		requests = append(requests, ctrl.Request{NamespacedName: key})
	}
	return requests
}
