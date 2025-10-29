package reconciler

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/logfield"
)

type DeploymentReconciler struct {
	*Reconciler
}

var (
	_ patcher = &DeploymentReconciler{}
)

func NewDeploymentReconciler(r *Reconciler) *DeploymentReconciler {
	return &DeploymentReconciler{
		Reconciler: r,
	}
}

func (r *DeploymentReconciler) Reconcile(
	ctx context.Context,
	owner client.Object,
	desired appsv1.Deployment,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx)
	if err := r.reconcile(ctx, owner, &desired, r.patch, deps...); err != nil {
		logger.V(1).
			WithValues(logfield.ResourceKV(&desired)...).
			Error(err, "Failed to reconcile Deployment ")
		return fmt.Errorf("reconciling Deployment: %w", err)
	}
	return nil
}

func (r *DeploymentReconciler) Cleanup(
	ctx context.Context,
	owner client.Object,
	resourceName string,
) error {
	logger := log.FromContext(ctx)

	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: owner.GetNamespace(),
		Name:      resourceName,
	}, deployment)

	if apierrors.IsNotFound(err) {
		logger.V(1).Info("Deployment not found, skipping deletion", "name", resourceName)
		return nil
	}

	if err != nil {
		return fmt.Errorf("getting Deployment %s: %w", resourceName, err)
	}

	if !metav1.IsControlledBy(deployment, owner) {
		logger.V(1).Info("Deployment is not owned by controller, skipping deletion", "name", resourceName)
		return nil
	}

	if err := r.Delete(ctx, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("Deployment not found, skipping deletion", "name", resourceName)
			return nil
		}
		return fmt.Errorf("deleting Deployment %s: %w", resourceName, err)
	}

	logger.V(1).Info("Deployment deleted", "name", resourceName)
	return nil
}

func (r *DeploymentReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *appsv1.Deployment) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())
		dst.Spec.Replicas = src.Spec.Replicas
		dst.Spec.Strategy = src.Spec.Strategy
		dst.Spec.Selector = src.Spec.Selector
		dst.Spec.Template.Spec = src.Spec.Template.Spec
		return res
	}
	return patchImpl(existing.(*appsv1.Deployment), desired.(*appsv1.Deployment)), nil
}
