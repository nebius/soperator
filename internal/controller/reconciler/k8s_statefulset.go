package reconciler

import (
	"context"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
)

type StatefulSetReconciler struct {
	*Reconciler
}

var (
	_ patcher = &StatefulSetReconciler{}
)

func NewStatefulSetReconciler(r *Reconciler) *StatefulSetReconciler {
	return &StatefulSetReconciler{
		Reconciler: r,
	}
}

func (r *StatefulSetReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *appsv1.StatefulSet,
	deps ...metav1.Object,
) error {
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile StatefulSet")
		return errors.Wrap(err, "reconciling StatefulSet")
	}
	return nil
}

func (r *StatefulSetReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(src, dst *appsv1.StatefulSet) client.Patch {
		res := client.MergeFrom(src.DeepCopy())

		src.Spec.Replicas = dst.Spec.Replicas
		src.Spec.UpdateStrategy = dst.Spec.UpdateStrategy
		src.Spec.VolumeClaimTemplates = append([]corev1.PersistentVolumeClaim{}, dst.Spec.VolumeClaimTemplates...)
		src.Spec.Template.Spec = dst.Spec.Template.Spec

		return res
	}
	return patchImpl(existing.(*appsv1.StatefulSet), desired.(*appsv1.StatefulSet)), nil
}
