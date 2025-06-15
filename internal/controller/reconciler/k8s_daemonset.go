package reconciler

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
)

type DaemonSetReconciler struct {
	*Reconciler
}

var (
	_ patcher = &DaemonSetReconciler{}
)

func NewDaemonSetReconciler(r *Reconciler) *DaemonSetReconciler {
	return &DaemonSetReconciler{
		Reconciler: r,
	}
}

func (r *DaemonSetReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *appsv1.DaemonSet,
	deps ...metav1.Object,
) error {
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile DaemonSet")
		return fmt.Errorf("reconciling DaemonSet: %w", err)
	}
	return nil
}

func (r *DaemonSetReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *appsv1.DaemonSet) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())
		dst.Spec.Template.Spec = src.Spec.Template.Spec

		return res
	}
	return patchImpl(existing.(*appsv1.DaemonSet), desired.(*appsv1.DaemonSet)), nil
}
