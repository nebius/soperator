package prometheus

import (
	"fmt"
	"log"

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
	clusterNamespace string,
	initContainers []corev1.Container,
	valuesExporter *values.SlurmExporter,
	nodeFilters []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
	matchLabels map[string]string,
	slurmAPIServer string,
) corev1.PodTemplateSpec {
	var volumes []corev1.Volume
	var annotations map[string]string
	if !valuesExporter.UseSoperatorExporter {
		volumes = []corev1.Volume{
			common.RenderVolumeJailFromSource(volumeSources, *valuesExporter.VolumeJail.VolumeSourceName),
			common.RenderVolumeSlurmConfigs(clusterName),
			common.RenderVolumeMungeKey(clusterName),
			common.RenderVolumeMungeSocket(),
		}
		annotations = map[string]string{
			fmt.Sprintf(
				"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameExporter,
			): valuesExporter.AppArmorProfile,
			fmt.Sprintf(
				"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameMunge,
			): valuesExporter.ContainerMunge.AppArmorProfile,
		}
	}

	var affinity *corev1.Affinity = nil
	var tolerations []corev1.Toleration
	var nodeSelector map[string]string

	nodeFilter, err := utils.GetBy(
		nodeFilters,
		valuesExporter.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)
	if err == nil {
		affinity = nodeFilter.Affinity
		nodeSelector = nodeFilter.NodeSelector
		tolerations = nodeFilter.Tolerations
	}

	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      matchLabels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			Affinity:       affinity,
			Tolerations:    tolerations,
			NodeSelector:   nodeSelector,
			InitContainers: initContainers,
			Containers: []corev1.Container{
				RenderContainerExporter(valuesExporter, clusterName, clusterNamespace, slurmAPIServer),
			},
			Volumes:            volumes,
			ServiceAccountName: buildExporterServiceAccountName(clusterName),
		},
	}
}

func RenderPodTemplateSpec(
	clusterName string,
	clusterNamespace string,
	initContainers []corev1.Container,
	valuesExporter *values.SlurmExporter,
	nodeFilters []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
	matchLabels map[string]string,
	podTemplateSpec *corev1.PodTemplateSpec,
	slurmAPIServer string,
) corev1.PodTemplateSpec {
	result := BasePodTemplateSpec(
		clusterName, clusterNamespace, initContainers, valuesExporter, nodeFilters, volumeSources, matchLabels, slurmAPIServer)
	if podTemplateSpec != nil {
		var err error
		result, err = common.MergePodTemplateSpecs(result, podTemplateSpec)
		if err != nil {
			log.Fatalf("Error performing strategic merge: %v", err)
		}
	}
	return result
}
