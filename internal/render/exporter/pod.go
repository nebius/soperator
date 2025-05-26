package exporter

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func renderPodTemplateSpec(
	clusterName string,
	clusterNamespace string,
	initContainers []corev1.Container,
	exporterValues values.SlurmExporter,
	nodeFilters []slurmv1.K8sNodeFilter,
	matchLabels map[string]string,
	podTemplatePatch *corev1.PodTemplateSpec,
	slurmAPIServer string,
) (corev1.PodTemplateSpec, error) {
	nodeFilter, err := utils.GetBy(
		nodeFilters,
		exporterValues.K8sNodeFilterName,
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
			Affinity:       nodeFilter.Affinity,
			Tolerations:    nodeFilter.Tolerations,
			NodeSelector:   nodeFilter.NodeSelector,
			InitContainers: initContainers,
			Containers: []corev1.Container{
				renderContainerExporter(exporterValues, clusterName, clusterNamespace, slurmAPIServer),
			},
			ServiceAccountName: buildExporterServiceAccountName(clusterName),
		},
	}
	if podTemplatePatch != nil {
		result, err = common.MergePodTemplateSpecs(result, podTemplatePatch)
		if err != nil {
			return corev1.PodTemplateSpec{}, fmt.Errorf("strategic merge: %w", err)
		}
	}
	return result, nil
}
