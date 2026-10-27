package reconciler

import (
	"context"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/logfield"
)

type HorizontalPodAutoscalerReconciler struct {
	*Reconciler
}

var _ patcher = &HorizontalPodAutoscalerReconciler{}

func NewHorizontalPodAutoscalerReconciler(r *Reconciler) *HorizontalPodAutoscalerReconciler {
	return &HorizontalPodAutoscalerReconciler{Reconciler: r}
}

func (r *HorizontalPodAutoscalerReconciler) Reconcile(
	ctx context.Context,
	owner client.Object,
	desired *autoscalingv2.HorizontalPodAutoscaler,
) error {
	if err := r.reconcile(ctx, owner, desired, r.patch); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile HorizontalPodAutoscaler")
		return fmt.Errorf("reconciling HorizontalPodAutoscaler: %w", err)
	}
	return nil
}

func (r *HorizontalPodAutoscalerReconciler) Cleanup(
	ctx context.Context,
	owner client.Object,
	resourceName string,
) error {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: owner.GetNamespace(),
		Name:      resourceName,
	}, hpa)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting HorizontalPodAutoscaler %s: %w", resourceName, err)
	}
	if !metav1.IsControlledBy(hpa, owner) {
		return nil
	}
	if err := r.Delete(ctx, hpa); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting HorizontalPodAutoscaler %s: %w", resourceName, err)
	}
	return nil
}

func (r *HorizontalPodAutoscalerReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	dst := existing.(*autoscalingv2.HorizontalPodAutoscaler)
	src := desired.(*autoscalingv2.HorizontalPodAutoscaler)
	res := client.MergeFrom(dst.DeepCopy())
	dst.Spec = *src.Spec.DeepCopy()
	return res, nil
}
