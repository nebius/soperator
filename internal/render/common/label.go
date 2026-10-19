package common

import (
	"nebius.ai/slurm-operator/internal/consts"
)

// RenderLabels prepends to the provided labels, the default set of labels used for all resources.
// These labels are recommended by k8s https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func RenderLabels(componentType consts.ComponentType, clusterName string) map[string]string {
	return map[string]string{
		consts.LabelNameKey:      consts.LabelNameValue,
		consts.LabelInstanceKey:  clusterName,
		consts.LabelComponentKey: componentType.String(),
		consts.LabelPartOfKey:    consts.LabelPartOfValue,
		consts.LabelManagedByKey: consts.LabelManagedByValue,
	}
}

// RenderMatchLabels prepends to the provided labels, the default set of match-labels used for all resources.
func RenderMatchLabels(componentType consts.ComponentType, clusterName string) map[string]string {
	return map[string]string{
		consts.LabelNameKey:      consts.LabelNameValue,
		consts.LabelInstanceKey:  clusterName,
		consts.LabelComponentKey: componentType.String(),
	}
}

// MergeExtraLabels adds extra into labels without overriding any key already present in labels.
// labels is expected to already contain every key the operator relies on (e.g. the ones used to
// build the resource's selector), so user-supplied extra labels can't collide with them and wedge
// an immutable selector on update.
func MergeExtraLabels(labels, extra map[string]string) {
	mergeExtra(labels, extra)
}

// MergeExtraAnnotations adds extra into annotations without overriding any key already present in
// annotations. annotations is expected to already contain every key the operator relies on (e.g.
// the ones other controllers use to find the pod), so user-supplied extra annotations can't hijack them.
func MergeExtraAnnotations(annotations, extra map[string]string) {
	mergeExtra(annotations, extra)
}

func mergeExtra(dst, extra map[string]string) {
	for k, v := range extra {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}
