package soperatorchecks

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
)

func RenderK8sJob(check *slurmv1alpha1.ActiveCheck, cronJob *batchv1.CronJob) *batchv1.Job {
	labels := common.RenderLabels(consts.ComponentTypeSoperatorChecks, check.Spec.SlurmClusterRefName)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      RenderK8sJobName(check),
			Namespace: check.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         cronJob.APIVersion,
					Kind:               cronJob.Kind,
					Name:               cronJob.Name,
					UID:                cronJob.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
			Labels:      labels,
			Annotations: cronJob.Spec.JobTemplate.Annotations,
		},
		Spec: *cronJob.Spec.JobTemplate.Spec.DeepCopy(),
	}
}

func RenderK8sJobName(check *slurmv1alpha1.ActiveCheck) string {
	return fmt.Sprintf("%s-manual-run", check.Spec.Name)
}
