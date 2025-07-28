package sconfigcontroller

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func BasePodTemplateSpec(
	clusterNamespace string,
	clusterName string,
	slurmAPIServer string,
	sConfigController *values.SConfigController,
	nodeFilters []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
	matchLabels map[string]string,
) (*corev1.PodTemplateSpec, error) {
	volumes := []corev1.Volume{
		common.RenderVolumeJailFromSource(volumeSources, *sConfigController.VolumeJail.VolumeSourceName),
	}

	nodeFilter, err := utils.GetBy(
		nodeFilters,
		sConfigController.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)
	if err != nil {
		return nil, err
	}

	var securityContext *corev1.PodSecurityContext = nil
	if sConfigController.RunAsUid != nil || sConfigController.RunAsGid != nil {
		securityContext = &corev1.PodSecurityContext{
			RunAsUser:  sConfigController.RunAsUid,
			RunAsGroup: sConfigController.RunAsGid,
		}
	}

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: matchLabels,
			Annotations: map[string]string{
				consts.DefaultContainerAnnotationName: consts.ContainerNameSConfigController,
			},
		},
		Spec: corev1.PodSpec{
			Affinity:     nodeFilter.Affinity,
			Tolerations:  nodeFilter.Tolerations,
			NodeSelector: nodeFilter.NodeSelector,
			Containers: []corev1.Container{
				renderContainerSConfigController(
					clusterNamespace,
					clusterName,
					slurmAPIServer,
					sConfigController.Container,
				),
			},
			InitContainers: []corev1.Container{
				renderInitContainerSConfigController(
					sConfigController.JailSlurmConfigPath,
					sConfigController.RunAsUid,
					sConfigController.RunAsGid,
				),
			},
			Volumes:            volumes,
			ServiceAccountName: naming.BuildServiceAccountSconfigControllerName(clusterName),
			SecurityContext:    securityContext,
		},
	}, nil
}
