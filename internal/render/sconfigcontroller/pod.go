package sconfigcontroller

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
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

	initContainers := []corev1.Container{
		renderInitContainerSConfigController(
			sConfigController.RunAsUid,
			sConfigController.RunAsGid,
		),
	}

	// Lexicographic sorting init containers by their names to have implicit ordering functionality
	sort.Slice(initContainers, func(i, j int) bool {
		return initContainers[i].Name < initContainers[j].Name
	})

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: matchLabels,
			Annotations: map[string]string{
				consts.AnnotationDefaultContainerName: consts.ContainerNameSConfigController,
			},
		},
		Spec: corev1.PodSpec{
			HostUsers:         sConfigController.HostUsers,
			Affinity:          nodeFilter.Affinity,
			Tolerations:       nodeFilter.Tolerations,
			NodeSelector:      nodeFilter.NodeSelector,
			PriorityClassName: sConfigController.PriorityClass,
			Containers: []corev1.Container{
				renderContainerSConfigController(
					clusterNamespace,
					clusterName,
					slurmAPIServer,
					sConfigController,
				),
			},
			InitContainers:     initContainers,
			Volumes:            volumes,
			ServiceAccountName: sConfigController.ServiceAccountName,
			SecurityContext:    securityContext,
		},
	}, nil
}
