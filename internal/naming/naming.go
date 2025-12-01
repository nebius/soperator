package naming

import (
	"fmt"
	"path"
	"strings"

	"nebius.ai/slurm-operator/internal/consts"
)

const (
	entityService = "svc"
)

type namedEntity struct {
	// clusterName is an optional name of the cluster.
	// empty if nothing
	clusterName string

	// componentType defines whether the entity belongs to some component.
	// nil if common
	componentType *consts.ComponentType

	// componentSpecifier defines whether the entity belongs to particular component.
	// empty if nothing
	componentSpecifier string

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
	if e.componentSpecifier != "" {
		es = append(es, e.componentSpecifier)
	}
	if e.entity != "" {
		es = append(es, e.entity)
	}

	return strings.Join(es, "-")
}

func BuildServiceName(componentType consts.ComponentType, clusterName string) string {
	return namedEntity{
		clusterName:   clusterName,
		componentType: &componentType,
		entity:        entityService,
	}.String()
}

func BuildNodeSetServiceName(clusterName string, nodeSetName string) string {
	return namedEntity{
		clusterName:        clusterName,
		componentType:      &consts.ComponentTypeNodeSet,
		componentSpecifier: nodeSetName,
		entity:             entityService,
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

func BuildNodeSetServiceFQDN(
	namespace,
	clusterName string,
	nodeSetName string,
) string {
	// <svcName>.<namespace>.svc.cluster.local
	svcName := BuildNodeSetServiceName(clusterName, nodeSetName)
	return fmt.Sprintf("%s.%s.svc.cluster.local", svcName, namespace)
}

func BuildNodeSetUmbrellaServiceFQDN(
	namespace,
	clusterName string,
) string {
	// <svcName>.<namespace>.svc.cluster.local
	svcName := BuildServiceName(consts.ComponentTypeNodeSet, clusterName)
	return fmt.Sprintf("%s.%s.svc.cluster.local", svcName, namespace)
}

func BuildServiceHostFQDN(
	componentType consts.ComponentType,
	namespace,
	clusterName string,
	hostIndex int32,
) (hostName, hostFQDN string) {
	// <stsName>-<index>.<svcName>.<namespace>.svc.cluster.local
	hostName = fmt.Sprintf("%s-%d", BuildStatefulSetName(componentType), hostIndex)
	hostFQDN = fmt.Sprintf("%s.%s", hostName, BuildServiceFQDN(componentType, namespace, clusterName))
	return hostName, hostFQDN
}

func BuildAppArmorProfileName(clusterName, namespace string) string {
	return namedEntity{
		clusterName: fmt.Sprintf("soperator-cluster-%s", clusterName),
		entity:      fmt.Sprintf("ns-%s", namespace),
	}.String()
}

func BuildStatefulSetName(componentType consts.ComponentType) string {
	return namedEntity{
		componentType: &componentType,
		clusterName:   "",
		entity:        "",
	}.String()
}

func BuildNodeSetStatefulSetName(nodeSetName string) string {
	return namedEntity{
		componentType: nil,
		clusterName:   "",
		entity:        nodeSetName,
	}.String()
}

func BuildDaemonSetName(componentType consts.ComponentType) string {
	return namedEntity{
		componentType: &componentType,
		clusterName:   "",
		entity:        "",
	}.String()
}

func BuildDeploymentName(componentType consts.ComponentType) string {
	return namedEntity{
		componentType: &componentType,
		entity:        "",
	}.String()
}

func BuildConfigMapSlurmConfigsName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameSlurmConfigs,
	}.String()
}

func BuildSecretMungeKeyName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.SecretMungeKeyName,
	}.String()
}

// region Login

func BuildConfigMapSSHDConfigsNameLogin(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameSSHDConfigsLogin,
	}.String()
}

func BuildSecretSSHDKeysName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.SecretSshdKeysName,
	}.String()
}

func BuildConfigMapSshRootPublicKeysName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameSshRootPublicKeys,
	}.String()
}

func BuildConfigMapSecurityLimitsName(componentType consts.ComponentType, clusterName string) string {
	return namedEntity{
		componentType: &componentType,
		clusterName:   clusterName,
		entity:        consts.ConfigMapNameSecurityLimits,
	}.String()
}

func BuildConfigMapSecurityLimitsForNodeSetName(clusterName, nodeSetName string) string {
	return namedEntity{
		clusterName:        clusterName,
		componentType:      &consts.ComponentTypeNodeSet,
		componentSpecifier: nodeSetName,
		entity:             consts.ConfigMapNameSecurityLimits,
	}.String()
}

func BuildLoginHeadlessServiceName(clusterName string) string {
	return namedEntity{
		componentType: &consts.ComponentTypeLogin,
		clusterName:   clusterName,
		entity:        "headless-svc",
	}.String()
}

func BuildLoginHeadlessServiceFQDN(namespace, clusterName string) string {
	svcName := BuildLoginHeadlessServiceName(clusterName)
	return fmt.Sprintf("%s.%s.svc.cluster.local", svcName, namespace)
}

// endregion Login

// region Worker
func BuildConfigMapSSHDConfigsNameWorker(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameSSHDConfigsWorker,
	}.String()
}

func BuildConfigMapSysctlName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameSysctl,
	}.String()
}

func BuildConfigMapSupervisordName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.ConfigMapNameSupervisord,
	}.String()
}

// endregion Worker

// region PopulateJailJob

func BuildPopulateJailJobName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.JobNamePopulateJail,
	}.String()
}

// endregion PopulateJailJob

func BuildVolumeMountSpoolPath(directory string) string {
	return path.Join(consts.VolumeMountPathSpool, directory)
}

func BuildServiceAccountWorkerName(clusterName string) string {
	return clusterName + "-worker-sa"
}

func BuildRoleWorkerName(clusterName string) string {
	return clusterName + "-worker-events-role"
}

func BuildServiceAccountActiveCheckName(clusterName string) string {
	return clusterName + "-activecheck-sa"
}

func BuildRoleActiveCheckName(clusterName string) string {
	return clusterName + "-activecheck-role"
}

func BuildRoleBindingActiveCheckName(clusterName string) string {
	return clusterName + "-activecheck-role-binding"
}

func BuildSecretSlurmdbdConfigsName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.SecretSlurmdbdConfigs,
	}.String()
}

func BuildSecretSlurmRESTSecretName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.SecretSlurmRESTJWTKey,
	}.String()
}

func BuildMariaDbName(clusterName string) string {
	return namedEntity{
		clusterName: clusterName,
		entity:      consts.MariaDbClusterSuffix,
	}.String()
}

func BuildConfigMapSbatchScriptName(scriptName string) string {
	return "sbatch-script-" + scriptName
}
