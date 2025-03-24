package soperatorchecks

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
)

func RenderK8sCronJob(check *slurmv1alpha1.ActiveCheck, foundPodTemplate *corev1.PodTemplate) (batchv1.CronJob, error) {
	labels := common.RenderLabels(consts.ComponentTypeSoperatorChecks, check.Spec.SlurmClusterRefName)

	var podTemplateSpec corev1.PodTemplateSpec

	basePodTemplateSpec := renderPodTemplateSpec(check, labels)

	if foundPodTemplate != nil {
		var err error
		podTemplateSpec, err = common.MergePodTemplateSpecs(basePodTemplateSpec, &foundPodTemplate.Template)
		if err != nil {
			return batchv1.CronJob{}, err
		}
	} else {
		podTemplateSpec = basePodTemplateSpec
	}

	return batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      check.Name,
			Namespace: check.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule: check.Spec.Schedule,
			Suspend:  ptr.To(check.Spec.Suspend),
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Parallelism:  ptr.To(int32(1)),
					Completions:  ptr.To(int32(1)),
					BackoffLimit: ptr.To(consts.ZeroReplicas),
					Template:     podTemplateSpec,
				},
			},
			SuccessfulJobsHistoryLimit: ptr.To(check.Spec.SuccessfulJobsHistoryLimit),
			FailedJobsHistoryLimit:     ptr.To(check.Spec.FailedJobsHistoryLimit),
		},
	}, nil
}
