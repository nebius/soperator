package reconciler

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"

	"nebius.ai/slurm-operator/internal/logfield"
)

type AdvancedStatefulSetReconciler struct {
	*Reconciler
}

var (
	_ patcher = &AdvancedStatefulSetReconciler{}
)

func NewAdvancedStatefulSetReconciler(r *Reconciler) *AdvancedStatefulSetReconciler {
	return &AdvancedStatefulSetReconciler{
		Reconciler: r,
	}
}

func (r *AdvancedStatefulSetReconciler) Reconcile(
	ctx context.Context,
	owner client.Object,
	desired *kruisev1b1.StatefulSet,
	deps ...metav1.Object,
) error {
	if err := r.reconcile(ctx, owner, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile StatefulSet")
		return fmt.Errorf("reconciling StatefulSet: %w", err)
	}
	return nil
}

func (r *AdvancedStatefulSetReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *kruisev1b1.StatefulSet) client.Patch {
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
		dst.Spec.Template.Spec = src.Spec.Template.Spec

		if len(src.Spec.VolumeClaimTemplates) > 0 {
			dst.Spec.VolumeClaimTemplates = append([]corev1.PersistentVolumeClaim{}, src.Spec.VolumeClaimTemplates...)
		}

		return res
	}
	return patchImpl(existing.(*kruisev1b1.StatefulSet), desired.(*kruisev1b1.StatefulSet)), nil
}
