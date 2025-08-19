package reconciler

import (
	"context"
	"fmt"

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
		return fmt.Errorf("reconciling StatefulSet: %w", err)
	}
	return nil
}

func (r *StatefulSetReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *appsv1.StatefulSet) client.Patch {
		original := dst.DeepCopy()

		res := client.MergeFrom(original)

		dst.Spec.Template.ObjectMeta.Labels = src.Spec.Template.ObjectMeta.Labels
		// Copy annotations from the desired StatefulSet to the existing StatefulSet
		// This is necessary because after the StatefulSet is created, patches recreate map of annotations and StatefulSet loses its annotations
		for k, v := range src.Spec.Template.ObjectMeta.Annotations {
			dst.Spec.Template.ObjectMeta.Annotations[k] = v
		}
		dst.Spec.Replicas = src.Spec.Replicas
		dst.Spec.UpdateStrategy = src.Spec.UpdateStrategy
		dst.Spec.VolumeClaimTemplates = append([]corev1.PersistentVolumeClaim{}, src.Spec.VolumeClaimTemplates...)
		dst.Spec.Template.Spec = src.Spec.Template.Spec

		return res
	}
	return patchImpl(existing.(*appsv1.StatefulSet), desired.(*appsv1.StatefulSet)), nil
}
