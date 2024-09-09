package prometheus

import (
	"encoding/json"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func BasePodTemplateSpec(
	clusterName string,
	munge *values.Container,
	valuesExporter *values.SlurmExporter,
	nodeFilters []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
	matchLabels map[string]string,
) corev1.PodTemplateSpec {
	volumes := []corev1.Volume{
		common.RenderVolumeJailFromSource(volumeSources, *valuesExporter.VolumeJail.VolumeSourceName),
		common.RenderVolumeSlurmConfigs(clusterName),
		common.RenderVolumeMungeKey(clusterName),
		common.RenderVolumeMungeSocket(),
	}

	var affinity *corev1.Affinity = nil
	var nodeSelector map[string]string

	nodeFilter, err := utils.GetBy(
		nodeFilters,
		valuesExporter.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)
	if err == nil {
		affinity = nodeFilter.Affinity
		nodeSelector = nodeFilter.NodeSelector
	}

	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: matchLabels,
			Annotations: map[string]string{
				fmt.Sprintf(
					"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameExporter,
				): consts.AnnotationApparmorValueUnconfined,
				fmt.Sprintf(
					"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNameMunge,
				): consts.AnnotationApparmorValueUnconfined,
			},
		},
		Spec: corev1.PodSpec{
			Affinity:     affinity,
			NodeSelector: nodeSelector,
			Containers: []corev1.Container{
				common.RenderContainerMunge(munge),
				RenderContainerExporter(valuesExporter),
			},
			Volumes: volumes,
		},
	}
}

func RenderPodTemplateSpec(
	clusterName string,
	munge *values.Container,
	valuesExporter *values.SlurmExporter,
	nodeFilters []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
	matchLabels map[string]string,
	podTemplateSpec *corev1.PodTemplateSpec,
) corev1.PodTemplateSpec {
	result := BasePodTemplateSpec(clusterName, munge, valuesExporter, nodeFilters, volumeSources, matchLabels)
	if podTemplateSpec != nil {
		// Convert the structs to JSON
		originalJSON, err := json.Marshal(result)
		if err != nil {
			log.Fatalf("Error marshalling original PodTemplateSpec: %v", err)
		}

		patchJSON, err := json.Marshal(podTemplateSpec)
		if err != nil {
			log.Fatalf("Error marshalling patch PodTemplateSpec: %v", err)
		}

		mergedJSON, err := strategicpatch.StrategicMergePatch(originalJSON, patchJSON, &corev1.PodTemplateSpec{})
		if err != nil {
			log.Fatalf("Error performing strategic merge: %v", err)
		}

		// Ummarshal the merged JSON back into a struct
		err = json.Unmarshal(mergedJSON, &result)
		if err != nil {
			log.Fatalf("Error unmarshalling merged PodTemplateSpec: %v", err)
		}
	}

	return result
}
