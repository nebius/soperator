package populate_jail

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderPopulateJailJob(
	namespace,
	clusterName string,
	nodeFilters []slurmv1.K8sNodeFilter,
	volumeSources []slurmv1.VolumeSource,
	populateJail *values.PopulateJail,
) (batchv1.Job, error) {
	labels := common.RenderLabels(consts.ComponentTypePopulateJail, clusterName)

	nodeFilter := utils.MustGetBy(
		nodeFilters,
		populateJail.K8sNodeFilterName,
		func(f slurmv1.K8sNodeFilter) string { return f.Name },
	)

	volumes := []corev1.Volume{
		common.RenderVolumeJailFromSource(volumeSources, *populateJail.VolumeJail.VolumeSourceName),
	}

	return batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind: "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      populateJail.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						fmt.Sprintf(
							"%s/%s", consts.AnnotationApparmorKey, consts.ContainerNamePopulateJail,
						): consts.AnnotationApparmorValueUnconfined,
					},
				},
				Spec: corev1.PodSpec{
					Affinity:      nodeFilter.Affinity,
					NodeSelector:  nodeFilter.NodeSelector,
					Tolerations:   nodeFilter.Tolerations,
					RestartPolicy: "Never",
					Volumes:       volumes,
					Containers:    []corev1.Container{renderContainerPopulateJail(populateJail)},
				},
			},
			Parallelism: ptr.To(int32(1)),
			Completions: ptr.To(int32(1)),
		},
	}, nil
}
