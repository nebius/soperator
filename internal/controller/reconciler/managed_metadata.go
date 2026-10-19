package reconciler

import (
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
)

// syncPodTemplateMetadata copies the metadata of the desired pod template into the existing one.
// The operator owns the pod labels entirely, while the annotations follow the syncManagedKeys semantics.
func syncPodTemplateMetadata(dstOwner, srcOwner metav1.Object, dst, src *corev1.PodTemplateSpec) {
	dst.Labels = maps.Clone(src.Labels)
	syncManagedKeys(dstOwner, srcOwner, consts.AnnotationManagedPodAnnotations, &dst.Annotations, src.Annotations)
}

// syncManagedKeys makes *dst carry every key of src and drops the keys that the operator rendered into it on
// the previous reconcile but doesn't render anymore. Keys the operator has never rendered (e.g.
// `kubectl.kubernetes.io/restartedAt`, the 'versions' annotation or ones injected by webhooks) are left untouched.
//
// The keys rendered by the operator are recorded in the `trackKey` annotation of dstOwner. The record is mirrored to
// srcOwner, so it survives when the whole resource is replaced with src instead of being patched.
// The record is empty on the first reconcile after the resource was created by an older operator version,
// so keys rendered before that can't be removed.
func syncManagedKeys(
	dstOwner, srcOwner metav1.Object,
	trackKey string,
	dst *map[string]string,
	src map[string]string,
) {
	for _, k := range splitManagedKeys(dstOwner.GetAnnotations()[trackKey]) {
		if _, rendered := src[k]; !rendered {
			delete(*dst, k)
		}
	}
	if len(src) > 0 {
		if *dst == nil {
			*dst = make(map[string]string, len(src))
		}
		maps.Copy(*dst, src)
	}

	record := joinManagedKeys(src)
	setOrDeleteAnnotation(dstOwner, trackKey, record)
	setOrDeleteAnnotation(srcOwner, trackKey, record)
}

func splitManagedKeys(record string) []string {
	if record == "" {
		return nil
	}
	return strings.Split(record, ",")
}

// joinManagedKeys serializes the keys of m, except the 'versions' annotation: it is maintained by the
// dependency versioning, which sets it on desired only when the resource is created or replaced.
func joinManagedKeys(m map[string]string) string {
	keys := slices.DeleteFunc(slices.Sorted(maps.Keys(m)), func(k string) bool {
		return k == consts.AnnotationVersions
	})
	return strings.Join(keys, ",")
}

func setOrDeleteAnnotation(obj metav1.Object, key, value string) {
	annotations := obj.GetAnnotations()
	if value == "" {
		if _, found := annotations[key]; found {
			delete(annotations, key)
			obj.SetAnnotations(annotations)
		}
		return
	}
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[key] = value
	obj.SetAnnotations(annotations)
}
