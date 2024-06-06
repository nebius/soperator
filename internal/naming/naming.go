package naming

import (
	"fmt"
	"path"
	"strings"

	"nebius.ai/slurm-operator/internal/consts"
)

const (
	entityService     = "svc"
	entityStatefulSet = "sts"
)

type namedEntity struct {
	clusterName string

	// componentType defines whether the entity belongs to some component.
	// nil if common
	componentType *consts.ComponentType

	entity string
}

func (e namedEntity) String() string {
	es := []string{e.clusterName}
	if e.componentType != nil {
		es = append(es, (*e.componentType).String())
	}
	es = append(es, e.entity)

	return strings.Join(es, "-")
}

func BuildServiceName(componentType consts.ComponentType, clusterName string) string {
	return namedEntity{
		componentType: &componentType,
		clusterName:   clusterName,
		entity:        entityService,
	}.String()
}

func BuildServiceReplicaFQDN(componentType consts.ComponentType, namespace, clusterName string, replicaIndex int32) (replicaName, replicaFQDN string) {
	serviceName := BuildServiceName(componentType, clusterName)
	replicaName = fmt.Sprintf("%s-%d", serviceName, replicaIndex)
	replicaFQDN = fmt.Sprintf("%s.%s.%s.svc.cluster.local", replicaName, serviceName, namespace)
	return replicaName, replicaFQDN
}

func BuildStatefulSetName(componentType consts.ComponentType, clusterName string) string {
	return namedEntity{
		componentType: &componentType,
		clusterName:   clusterName,
		entity:        entityStatefulSet,
	}.String()
}

func BuildConfigMapSlurmConfigsName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapSlurmConfigsName,
	}.String()
}

func BuildVolumeMountSpoolPath(directory string) string {
	return path.Join(consts.VolumeSpoolMountPath, directory)
}
