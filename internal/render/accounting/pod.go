package accounting

import (
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils/sliceutils"
	"nebius.ai/slurm-operator/internal/values"
)

func BasePodTemplateSpec(
	clusterName string,
	accounting *values.SlurmAccounting,
	nodeFilters []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
	matchLabels map[string]string,
) (*corev1.PodTemplateSpec, error) {
	volumes := []corev1.Volume{
		common.RenderVolumeJailFromSource(volumeSources, *accounting.VolumeJail.VolumeSourceName),
		common.RenderVolumeProjectedSlurmConfigs(
			clusterName,
			RenderVolumeProjectionSlurmdbdConfigs(clusterName),
		),
		common.RenderVolumeMungeKey(clusterName),
		common.RenderVolumeRESTJWTKey(clusterName),
		common.RenderVolumeMungeSocket(),
		RenderVolumeSlurmdbdSpool(accounting),
	}

	additionalVolumeMounts := make([]corev1.VolumeMount, 0)

	if accounting.ExternalDB.Enabled {
		if accounting.ExternalDB.TLS.ServerCASecretRef != "" {
			volumes = append(volumes,
				RenderVolumeSlurmdbdSSLCACertificate(accounting.ExternalDB.TLS.ServerCASecretRef))
			additionalVolumeMounts = append(additionalVolumeMounts,
				RenderVolumeMountSlurmdbdSSLCACertificate())
		}

		if accounting.ExternalDB.TLS.ClientCertSecretRef != "" {
			volumes = append(volumes,
				RenderVolumeSlurmdbdSSLClientKey(accounting.ExternalDB.TLS.ClientCertSecretRef))
			additionalVolumeMounts = append(additionalVolumeMounts,
				RenderVolumeMountSlurmdbdSSLClientKey())
		}
	}

	nodeFilter, err := sliceutils.GetBy(
		nodeFilters,
		accounting.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)
	if err != nil {
		return nil, err
	}

	initContainers := slices.Clone(accounting.CustomInitContainers)
	initContainers = append(initContainers,
		renderContainerDbwaiter(clusterName, accounting),
		common.RenderContainerMunge(&accounting.ContainerMunge),
	)

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      matchLabels,
			Annotations: common.RenderDefaultContainerAnnotation(consts.ContainerNameAccounting),
		},
		Spec: corev1.PodSpec{
			HostUsers:         accounting.HostUsers,
			Affinity:          nodeFilter.Affinity,
			Tolerations:       nodeFilter.Tolerations,
			NodeSelector:      nodeFilter.NodeSelector,
			Hostname:          consts.HostnameAccounting,
			PriorityClassName: accounting.PriorityClass,
			InitContainers:    initContainers,
			Containers: []corev1.Container{
				renderContainerAccounting(accounting.ContainerAccounting, additionalVolumeMounts),
			},
			Volumes: volumes,
		},
	}, nil
}
