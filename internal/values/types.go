package values

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// region Service

type Service struct {
	slurmv1.NodeService

	Name        string
	ServiceType corev1.ServiceType
	Protocol    corev1.Protocol
}

func buildServiceFrom(
	svc slurmv1.NodeService,
	name string,
) Service {
	return Service{
		NodeService: *svc.DeepCopy(),
		Name:        name,
		ServiceType: corev1.ServiceTypeClusterIP,
		Protocol:    corev1.ProtocolTCP,
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

// endregion StatefulSet

// region ConfigMap

type K8sConfigMap struct {
	Name string
}

func buildSlurmConfigFrom(cluster *slurmv1.SlurmCluster) K8sConfigMap {
	return K8sConfigMap{Name: naming.BuildConfigMapSlurmConfigsName(cluster.Name)}
}

// endregion ConfigMap

// region CR version

func buildCRVersionFrom(ctx context.Context, cluster *slurmv1.SlurmCluster) string {
	logger := log.FromContext(ctx)

	if cluster.Spec.CRVersion == "" {
		logger.Info(
			"CR version is empty, using default",
			"Slurm.CR.DefaultVersion", consts.VersionCR)
		return consts.VersionCR
	}

	return cluster.Spec.CRVersion
}

// endregion CR version

// region K8sNodeFilter

func buildNodeFiltersFrom(cluster *slurmv1.SlurmCluster) []slurmv1.K8sNodeFilter {
	res := make([]slurmv1.K8sNodeFilter, len(cluster.Spec.K8sNodeFilters))
	for i, nodeFilter := range cluster.Spec.K8sNodeFilters {
		res[i] = *nodeFilter.DeepCopy()
	}
	return res
}

// endregion K8sNodeFilter

// region Volume

func buildVolumeSourcesFrom(cluster *slurmv1.SlurmCluster) []slurmv1.VolumeSource {
	res := make([]slurmv1.VolumeSource, len(cluster.Spec.VolumeSources))
	for i, volumeSource := range cluster.Spec.VolumeSources {
		res[i].Name = volumeSource.Name
		res[i].VolumeSource = *volumeSource.VolumeSource.DeepCopy()
	}
	return res
}

func buildVolumeFrom(volume *slurmv1.NodeVolume) slurmv1.NodeVolume {
	return *volume.DeepCopy()
}

type PVCTemplateSpec struct {
	Name string
	Spec *corev1.PersistentVolumeClaimSpec
}

// endregion Volume

// region Secret

func buildSecretsFrom(cluster *slurmv1.SlurmCluster) slurmv1.Secrets {
	return *cluster.Spec.Secrets.DeepCopy()
}

// endregion Secret
