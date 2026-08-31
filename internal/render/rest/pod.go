package rest

import (
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func BasePodTemplateSpec(
	clusterName string,
	valuesREST *values.SlurmREST,
	nodeFilters []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
	matchLabels map[string]string,
) (*corev1.PodTemplateSpec, error) {
	volumes := []corev1.Volume{
		common.RenderVolumeSlurmConfigs(clusterName),
		common.RenderVolumeJailFromSource(volumeSources, *valuesREST.VolumeJail.VolumeSourceName),
	}

	nodeFilter, err := utils.GetBy(
		nodeFilters,
		valuesREST.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)
	if err != nil {
		return nil, err
	}

	labels := maps.Clone(matchLabels)
	common.MergeExtraLabels(labels, valuesREST.Labels)

	annotations := common.RenderDefaultContainerAnnotation(consts.ContainerNameREST)
	maps.Copy(annotations, valuesREST.Annotations)

	res := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			HostUsers:         valuesREST.HostUsers,
			ImagePullSecrets:  valuesREST.ContainerREST.ImagePullSecrets,
			Affinity:          nodeFilter.Affinity,
			Tolerations:       nodeFilter.Tolerations,
			NodeSelector:      nodeFilter.NodeSelector,
			Hostname:          consts.HostnameREST,
			InitContainers:    valuesREST.CustomInitContainers,
			Containers:        []corev1.Container{renderContainerREST(valuesREST)},
			Volumes:           volumes,
			PriorityClassName: valuesREST.PriorityClass,
		},
	}

	return res, nil
}
