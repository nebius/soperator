package exporter

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func renderPodTemplateSpec(
	clusterValues *values.SlurmCluster,
	initContainers []corev1.Container,
	matchLabels map[string]string,
) corev1.PodTemplateSpec {
	nodeFilter, err := utils.GetBy(
		clusterValues.NodeFilters,
		clusterValues.SlurmExporter.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)
	if err != nil {
		_ = err // Ignore not found error, use "empty" node filter.
		nodeFilter = slurmv1.K8sNodeFilter{}
	}
	result := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: matchLabels,
		},
		Spec: corev1.PodSpec{
			HostUsers:          clusterValues.SlurmExporter.HostUsers,
			Affinity:           nodeFilter.Affinity,
			Tolerations:        nodeFilter.Tolerations,
			NodeSelector:       nodeFilter.NodeSelector,
			PriorityClassName:  clusterValues.SlurmExporter.PriorityClass,
			InitContainers:     initContainers,
			Containers:         []corev1.Container{renderContainerExporter(clusterValues)},
			ServiceAccountName: clusterValues.SlurmExporter.ServiceAccountName,
			Volumes:            []corev1.Volume{},
		},
	}
	return result
}
