package reconciler

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
)

type ServiceAccountReconciler struct {
	*Reconciler
}

var (
	_ patcher = &ServiceAccountReconciler{}
)

func NewServiceAccountReconciler(r *Reconciler) *ServiceAccountReconciler {
	return &ServiceAccountReconciler{
		Reconciler: r,
	}
}

func (r *ServiceAccountReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *corev1.ServiceAccount,
	deps ...metav1.Object,
) error {
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile ServiceAccount")
		return fmt.Errorf("reconciling ServiceAccount: %w", err)
	}
	return nil
}

func (r *ServiceAccountReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *corev1.ServiceAccount) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		dst.Name = src.Name

		return res
	}
	return patchImpl(existing.(*corev1.ServiceAccount), desired.(*corev1.ServiceAccount)), nil
}
