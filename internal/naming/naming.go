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
	// clusterName is an optional name of the cluster.
	// empty if nothing
	clusterName string

	// componentType defines whether the entity belongs to some component.
	// nil if common
	componentType *consts.ComponentType

	// entity is an optional K8S resource marker (e.g. "sts", "svc", etc.)
	// empty if nothing
	entity string
}

func (e namedEntity) String() string {
	var es []string
	if e.clusterName != "" {
		es = append(es, e.clusterName)
	}
	if e.componentType != nil {
		es = append(es, (*e.componentType).String())
	}
	if e.entity != "" {
		es = append(es, e.entity)
	}

	return strings.Join(es, "-")
}

func BuildServiceName(componentType consts.ComponentType, clusterName string) string {
	return namedEntity{
		componentType: &componentType,
		clusterName:   clusterName,
		entity:        entityService,
	}.String()
}

func BuildServiceFQDN(
	componentType consts.ComponentType,
	namespace,
	clusterName string,
) string {
	// <svcName>.<namespace>.svc.cluster.local
	svcName := BuildServiceName(componentType, clusterName)
	return fmt.Sprintf("%s.%s.svc.cluster.local", svcName, namespace)
}

func BuildServiceHostFQDN(
	componentType consts.ComponentType,
	namespace,
	clusterName string,
	hostIndex int32,
) (hostName, hostFQDN string) {
	// <stsName>-<index>.<svcName>.<namespace>.svc.cluster.local
	hostName = fmt.Sprintf("%s-%d", BuildStatefulSetName(componentType, clusterName), hostIndex)
	hostFQDN = fmt.Sprintf("%s.%s", hostName, BuildServiceFQDN(componentType, namespace, clusterName))
	return hostName, hostFQDN
}

func BuildStatefulSetName(componentType consts.ComponentType, clusterName string) string {
	return namedEntity{
		componentType: &componentType,
		clusterName:   "",
		entity:        "",
	}.String()
}

func BuildConfigMapSlurmConfigsName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameSlurmConfigs,
	}.String()
}

// region Login

func BuildConfigMapSSHConfigsName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameSSHConfigs,
	}.String()
}

func BuildConfigMapSecurityLimitsName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameSecurityLimits,
	}.String()
}

// endregion Login

// region Worker

func BuildConfigMapNCCLTopologyName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameNCCLTopology,
	}.String()
}

func BuildConfigMapSysctlName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameSysctl,
	}.String()
}

// endregion Worker

func BuildCronJobNCCLBenchmarkName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.CronJobNameNCCLBenchmark,
	}.String()
}

func BuildPopulateJailJobName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.JobNamePopulateJail,
	}.String()
}

func BuildVolumeMountSpoolPath(directory string) string {
	return path.Join(consts.VolumeMountPathSpool, directory)
}
