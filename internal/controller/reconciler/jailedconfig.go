package reconciler

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/logfield"
)

type JailedConfigReconciler struct {
	*Reconciler
}

var (
	_ patcher = &JailedConfigReconciler{}
)

func NewJailedConfigReconciler(r *Reconciler) *JailedConfigReconciler {
	return &JailedConfigReconciler{
		Reconciler: r,
	}
}

func (r *JailedConfigReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *slurmv1alpha1.JailedConfig,
	deps ...metav1.Object,
) error {
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile JailedConfig")
		return fmt.Errorf("reconciling JailedConfig: %w", err)
	}
	return nil
}

func (r *JailedConfigReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *slurmv1alpha1.JailedConfig) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		// TODO is this correct?
		dst.Spec = src.Spec

		return res
	}
	return patchImpl(existing.(*slurmv1alpha1.JailedConfig), desired.(*slurmv1alpha1.JailedConfig)), nil
}
