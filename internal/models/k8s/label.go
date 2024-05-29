package k8smodels

import (
	"nebius.ai/slurm-operator/internal/consts"
)

// BuildClusterDefaultLabels prepends to the provided labels, the default set of labels used for all resources.
// These labels are recommended by k8s https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func BuildClusterDefaultLabels(clusterName string, componentType consts.ComponentType) map[string]string {
	return map[string]string{
		consts.LabelNameKey:      consts.LabelNameValue,
		consts.LabelInstanceKey:  clusterName,
		consts.LabelComponentKey: consts.ComponentNameByType[componentType],
		consts.LabelPartOfKey:    consts.LabelPartOfValue,
		consts.LabelManagedByKey: consts.LabelManagedByValue,
	}
}

// BuildClusterDefaultMatchLabels prepends to the provided labels, the default set of match-labels used for all resources.
func BuildClusterDefaultMatchLabels(clusterName string, componentType consts.ComponentType) map[string]string {
	return map[string]string{
		consts.LabelNameKey:      consts.LabelNameValue,
		consts.LabelInstanceKey:  clusterName,
		consts.LabelComponentKey: consts.ComponentNameByType[componentType],
	}
}
