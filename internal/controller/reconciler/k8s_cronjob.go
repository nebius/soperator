package reconciler

import (
	"context"
	"fmt"
	"maps"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/logfield"
)

type CronJobReconciler struct {
	*Reconciler
}

var (
	_ patcher = &CronJobReconciler{}
)

func NewCronJobReconciler(r *Reconciler) *CronJobReconciler {
	return &CronJobReconciler{
		Reconciler: r,
	}
}

func (r *CronJobReconciler) Reconcile(
	ctx context.Context,
	owner client.Object,
	desired *batchv1.CronJob,
	deps ...metav1.Object,
) error {
	if err := r.reconcile(ctx, owner, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile CronJob")
		return fmt.Errorf("reconciling CronJob: %w", err)
	}
	return nil
}

func (r *CronJobReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *batchv1.CronJob) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		dst.Spec.Schedule = src.Spec.Schedule
		dst.Spec.Suspend = src.Spec.Suspend
		dst.Spec.SuccessfulJobsHistoryLimit = src.Spec.SuccessfulJobsHistoryLimit
		dst.Spec.FailedJobsHistoryLimit = src.Spec.FailedJobsHistoryLimit
		syncManagedKeys(dst, src, consts.AnnotationManagedLabels, &dst.Labels, src.Labels)
		dst.Spec.JobTemplate.Labels = maps.Clone(src.Spec.JobTemplate.Labels)
		dst.Spec.JobTemplate.Annotations = maps.Clone(src.Spec.JobTemplate.Annotations)
		syncPodTemplateMetadata(dst, src, &dst.Spec.JobTemplate.Spec.Template, &src.Spec.JobTemplate.Spec.Template)
		dst.Spec.JobTemplate.Spec.Template.Spec = src.Spec.JobTemplate.Spec.Template.Spec

		return res
	}
	return patchImpl(existing.(*batchv1.CronJob), desired.(*batchv1.CronJob)), nil
}
