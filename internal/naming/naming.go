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

func BuildServiceHostFQDN(
	componentType consts.ComponentType,
	namespace,
	clusterName string,
	hostIndex int32,
) (hostName, hostFQDN string) {
	// <stsName>-<index>.<svcName>.<namespace>.svc.cluster.local
	stsName := BuildStatefulSetName(componentType, clusterName)
	svcName := BuildServiceName(componentType, clusterName)
	hostName = fmt.Sprintf("%s-%d", stsName, hostIndex)
	hostFQDN = fmt.Sprintf("%s.%s.%s.svc.cluster.local", hostName, svcName, namespace)
	return hostName, hostFQDN
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
		entity:      consts.ConfigMapNameSlurmConfigs,
	}.String()
}

func BuildConfigMapSSHConfigsName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameSSHConfigs,
	}.String()
}

func BuildConfigMapNCCLTopologyName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameNCCLTopology,
	}.String()
}

func BuildCronJobNCCLBenchmarkName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.CronJobNameNCCLBenchmark,
	}.String()
}

func BuildVolumeMountSpoolPath(directory string) string {
	return path.Join(consts.VolumeMountPathSpool, directory)
}
