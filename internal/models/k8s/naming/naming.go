package k8snaming

import (
	"fmt"

	"nebius.ai/slurm-operator/internal/consts"
)

func BuildServiceName(clusterName string, componentType consts.ComponentType) string {
	return fmt.Sprintf("%s-%s-svc", clusterName, consts.ComponentNameByType[componentType])
}

func BuildServiceFQDN(namespace, clusterName string, componentType consts.ComponentType) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", BuildServiceName(clusterName, componentType), namespace)
}

func BuildStatefulSetName(clusterName string, componentType consts.ComponentType) string {
	return fmt.Sprintf("%s-%s-sts", clusterName, consts.ComponentNameByType[componentType])
}

func BuildPVCName(clusterName string, componentType consts.ComponentType) string {
	return fmt.Sprintf("%s-%s-pvc", clusterName, consts.ComponentNameByType[componentType])
}

func BuildVolumeNameConfigs(clusterName string, componentType consts.ComponentType) string {
	return fmt.Sprintf("%s-%s-volume-configs", clusterName, consts.ComponentNameByType[componentType])
}

func BuildVolumeNameKey(clusterName string, componentType consts.ComponentType) string {
	return fmt.Sprintf("%s-%s-volume-key", clusterName, consts.ComponentNameByType[componentType])
}
