package reconciler

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
)

type ConfigMapReconciler struct {
	*Reconciler
}

var (
	_ patcher = &ConfigMapReconciler{}
)

func NewConfigMapReconciler(r *Reconciler) *ConfigMapReconciler {
	return &ConfigMapReconciler{
		Reconciler: r,
	}
}

func (r *ConfigMapReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *corev1.ConfigMap,
	deps ...metav1.Object,
) error {
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile ConfigMap")
		return errors.Wrap(err, "reconciling ConfigMap")
	}
	return nil
}

func (r *ConfigMapReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(e, d *corev1.ConfigMap) client.Patch {
		res := client.MergeFrom(e.DeepCopy())

		e.Data = d.Data

		return res
	}
	return patchImpl(existing.(*corev1.ConfigMap), desired.(*corev1.ConfigMap)), nil
}
