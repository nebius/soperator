package reconciler

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
)

type JobReconciler struct {
	*Reconciler
}

var (
	_ patcher = &JobReconciler{}
)

func NewJobReconciler(r *Reconciler) *JobReconciler {
	return &JobReconciler{
		Reconciler: r,
	}
}

func (r *JobReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *batchv1.Job,
	deps ...metav1.Object,
) error {
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile Job")
		return fmt.Errorf("reconciling Job: %w", err)
	}
	return nil
}

func (r *JobReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *batchv1.Job) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		// Dst.Spec.Template is immutable, so we need to copy the desired template to the existing one
		// just mutating the fields we need
		dst.Spec.Parallelism = src.Spec.Parallelism
		dst.Spec.Completions = src.Spec.Completions

		return res
	}
	return patchImpl(existing.(*batchv1.Job), desired.(*batchv1.Job)), nil
}
