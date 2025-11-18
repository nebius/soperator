package values

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

// region Container

type Container struct {
	slurmv1.NodeContainer

	Name string
}

func buildContainerFrom(
	container slurmv1.NodeContainer,
	name string,
) Container {
	return Container{
		NodeContainer: *container.DeepCopy(),
		Name:          name,
	}
}

// endregion Container

// region Service

type Service struct {
	Name                     string
	Type                     corev1.ServiceType
	Annotations              map[string]string
	LoadBalancerIP           string
	LoadBalancerSourceRanges []string
	NodePort                 int32
	Protocol                 corev1.Protocol
}

func buildServiceFrom(
	name string,
) Service {
	return Service{
		Name:                     name,
		Type:                     corev1.ServiceTypeClusterIP,
		Annotations:              map[string]string{},
		LoadBalancerIP:           "",
		LoadBalancerSourceRanges: []string{},
		NodePort:                 0,
		Protocol:                 corev1.ProtocolTCP,
	}
}

// endregion Service

// region StatefulSet

type StatefulSet struct {
	Name           string
	Replicas       int32
	MaxUnavailable intstr.IntOrString
}

func buildStatefulSetFrom(
	name string,
	size int32,
) StatefulSet {
	return StatefulSet{
		Name:           name,
		Replicas:       size,
		MaxUnavailable: intstr.FromInt32(1),
	}
}

func buildStatefulSetWithMaxUnavailableFrom(
	name string,
	size int32,
	maxUnavailable *intstr.IntOrString,
) StatefulSet {
	result := StatefulSet{
		Name:     name,
		Replicas: size,
	}

	if maxUnavailable != nil {
		result.MaxUnavailable = *maxUnavailable
	}

	return result
}

// endregion StatefulSet

// region DaemonSet

type DaemonSet struct {
	Name string
}

func buildDaemonSetFrom(name string) DaemonSet {
	return DaemonSet{
		Name: name,
	}
}

// endregion DaemonSet

// region CR version

func buildCRVersionFrom(ctx context.Context, crVersion string) string {
	logger := log.FromContext(ctx)

	if crVersion == "" {
		logger.V(1).Info(
			"CR version is empty, using default",
			"Slurm.CR.DefaultVersion", consts.VersionCR)
		return consts.VersionCR
	}

	return crVersion
}

// endregion CR version

// region K8sNodeFilter

func buildNodeFiltersFrom(nodeFilters []slurmv1.K8sNodeFilter) []slurmv1.K8sNodeFilter {
	res := make([]slurmv1.K8sNodeFilter, len(nodeFilters))
	for i, nodeFilter := range nodeFilters {
		res[i] = *nodeFilter.DeepCopy()
	}
	return res
}

// endregion K8sNodeFilter

// region Volume

func buildVolumeSourcesFrom(volumeSources []slurmv1.VolumeSource) []slurmv1.VolumeSource {
	res := make([]slurmv1.VolumeSource, len(volumeSources))
	for i, volumeSource := range volumeSources {
		res[i].Name = volumeSource.Name
		res[i].VolumeSource = *volumeSource.VolumeSource.DeepCopy()
	}
	return res
}

type PVCTemplateSpec struct {
	Name string
	Spec *corev1.PersistentVolumeClaimSpec
}

// endregion Volume

// region Secret

func BuildSecretsFrom(secrets *slurmv1.Secrets) slurmv1.Secrets {
	return *secrets.DeepCopy()
}

// endregion Secret

// region Deployment
type Deployment struct {
	Name            string
	Replicas        int32
	Strategy        appsv1.DeploymentStrategyType
	PodTemplateSpec *corev1.PodTemplateSpec
}

func buildDeploymentFrom(
	name string,
) Deployment {
	return Deployment{
		Name:            name,
		Replicas:        int32(1),
		Strategy:        appsv1.RecreateDeploymentStrategyType,
		PodTemplateSpec: nil,
	}
}

// endregion Deployment
