package rebooter

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodCacheTransform strips non-essential fields from a Pod before it is stored
// in the controller-runtime cache, reducing baseline memory usage.
//
// Only the minimum fields required by the rebooter are retained:
//   - Name / Namespace / UID — basic pod identity.
//   - OwnerReferences — used by IsControlledByDaemonSet.
//   - Spec.NodeName — used as the field-indexer key ("spec.nodeName").
//   - Spec.Tolerations — used by HasTolerationForNoExecute / HasTolerationForExists.
//
// All other ObjectMeta fields (ManagedFields, Annotations, Labels, …) and all
// Spec / Status fields are explicitly discarded.  Slices are shallow-copied so
// the stripped pod does not share underlying arrays with the original object.
func PodCacheTransform(i any) (any, error) { //nolint:unparam // error is always nil but required by cache.ByObject.Transform signature
	pod, ok := i.(*corev1.Pod)
	if !ok {
		return i, nil
	}

	ownerRefs := make([]metav1.OwnerReference, len(pod.OwnerReferences))
	copy(ownerRefs, pod.OwnerReferences)

	tolerations := make([]corev1.Toleration, len(pod.Spec.Tolerations))
	copy(tolerations, pod.Spec.Tolerations)

	return &corev1.Pod{
		TypeMeta: pod.TypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:            pod.Name,
			Namespace:       pod.Namespace,
			UID:             pod.UID,
			OwnerReferences: ownerRefs,
		},
		Spec: corev1.PodSpec{
			NodeName:    pod.Spec.NodeName,
			Tolerations: tolerations,
		},
	}, nil
}
