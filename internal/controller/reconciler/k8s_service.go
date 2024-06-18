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

type ServiceReconciler struct {
	*Reconciler
}

var (
	_ patcher = &ServiceReconciler{}
)

func NewServiceReconciler(r *Reconciler) *ServiceReconciler {
	return &ServiceReconciler{
		Reconciler: r,
	}
}

func (r *ServiceReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *corev1.Service,
	deps ...metav1.Object,
) error {
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile Service")
		return errors.Wrap(err, "reconciling Service")
	}
	return nil
}

func (r *ServiceReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(src, dst *corev1.Service) client.Patch {
		res := client.MergeFrom(src.DeepCopy())

		src.Spec.Type = dst.Spec.Type
		src.Spec.Ports = append([]corev1.ServicePort{}, dst.Spec.Ports...)

		return res
	}
	return patchImpl(existing.(*corev1.Service), desired.(*corev1.Service)), nil
}
