package reconciler

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
	cluster *slurmv1.SlurmCluster,
	desired *appsv1.Deployment,
	name *string,
	deps ...metav1.Object,
) error {
	if desired == nil {
		// If desired is nil, delete the Deployment
		if name == nil {
			log.FromContext(ctx).Info("Deployment is not needed, skipping deletion")
			return nil
		}
		log.FromContext(ctx).Info(fmt.Sprintf(
			"Deleting Deployment %s-collector, because of Deployment  is not needed", cluster.Name,
		))
		return r.deleteDeploymentIfOwnedByController(ctx, cluster, cluster.Namespace, *name)
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile Deployment ")
		return errors.Wrap(err, "reconciling Deployment ")
	}
	return nil
}

func (r *DeploymentReconciler) deleteDeploymentIfOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	namespace,
	name string,
) error {
	deployment, err := r.getDeployment(ctx, namespace, name)
	if err != nil {
		return errors.Wrap(err, "getting Deployment")
	}

	if !metav1.IsControlledBy(deployment, cluster) {
		log.FromContext(ctx).Info("Deployment is not owned by controller, skipping deletion")
		return nil
	}

	if err := r.Delete(ctx, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("Deployment already deleted")
			return nil
		}
		return errors.Wrap(err, "deleting Deployment")
	}
	log.FromContext(ctx).Info("Deployment deleted")
	return nil
}

func (r *DeploymentReconciler) getDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
		deployment,
	)
	return deployment, err
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
