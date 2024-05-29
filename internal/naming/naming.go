package naming

import (
	"fmt"

	"nebius.ai/slurm-operator/internal/consts"
)

type namedEntity struct {
	// componentType defines whether the entity belongs to some component.
	// nil if common
	componentType *consts.ComponentType

	clusterName string
	entityName  string
}

func (e namedEntity) String() string {
	if e.componentType == nil {
		return fmt.Sprintf("%s-%s", e.clusterName, e.entityName)
	}
	return fmt.Sprintf("%s-%s-%s", e.clusterName, consts.ComponentNameByType[*e.componentType], e.entityName)
}

func BuildServiceName(componentType consts.ComponentType, clusterName, svcName string) string {
	return namedEntity{
		componentType: &componentType,
		clusterName:   clusterName,
		entityName:    svcName,
	}.String()
}

func BuildServiceFQDN(componentType consts.ComponentType, namespace, clusterName, serviceName string, replicaIndex int32) (instanceName, instanceFQDN string) {
	fullServiceName := BuildServiceName(componentType, clusterName, serviceName)
	instanceName = fmt.Sprintf("%s-%d", fullServiceName, replicaIndex)
	instanceFQDN = fmt.Sprintf("%s.%s.%s.svc.cluster.local", instanceName, fullServiceName, namespace)
	return instanceName, instanceFQDN
}

func BuildStatefulSetName(componentType consts.ComponentType, clusterName, stsName string) string {
	return namedEntity{
		componentType: &componentType,
		clusterName:   clusterName,
		entityName:    stsName,
	}.String()
}

func BuildConfigMapSlurmConfigsName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entityName:  consts.ConfigMapSlurmConfigsName,
	}.String()
}

func BuildPVCName(clusterName string, componentType consts.ComponentType) string {
	return fmt.Sprintf("%s-%s-pvc", clusterName, consts.ComponentNameByType[componentType])
}

func BuildVolumeMountSpoolPath(componentType consts.ComponentType) (string, error) {
	var serviceName string
	switch componentType {
	case consts.ComponentTypeController:
		serviceName = consts.ServiceControllerName
	case consts.ComponentTypeWorker:
		serviceName = consts.ServiceWorkerName
	default:
		return "", fmt.Errorf("failed to build spool volume mount path for unknown component type: %q", componentType)
	}
	return fmt.Sprintf("%s/%s", consts.VolumeSpoolMountPath, serviceName), nil
}
