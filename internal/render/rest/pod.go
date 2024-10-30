package rest

import (
	"fmt"

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
	matchLabels map[string]string,
) (*corev1.PodTemplateSpec, error) {
	volumes := []corev1.Volume{common.RenderVolumeSlurmConfigs(clusterName)}

	var affinity *corev1.Affinity = nil
	var nodeSelector map[string]string

	nodeFilter, err := utils.GetBy(
		nodeFilters,
		valuesREST.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)
	if err != nil {
		return nil, err
	}

	affinity = nodeFilter.Affinity
	nodeSelector = nodeFilter.NodeSelector

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: matchLabels,
			Annotations: map[string]string{
				fmt.Sprintf(
					"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameREST,
				): valuesREST.ContainerREST.AppArmorProfile,
			},
		},
		Spec: corev1.PodSpec{
			Affinity:     affinity,
			NodeSelector: nodeSelector,
			Hostname:     consts.HostnameREST,
			Containers:   []corev1.Container{renderContainerREST(valuesREST.ContainerREST)},
			Volumes:      volumes,
		},
	}, nil
}
