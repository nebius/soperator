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
	for k, v := range extra {
		if _, exists := labels[k]; !exists {
			labels[k] = v
		}
	}
}
