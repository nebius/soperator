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
	deps ...metav1.Object,
) error {
	if desired == nil {
		// If desired is nil, delete the Deployment
		log.FromContext(ctx).Info(fmt.Sprintf(
			"Deleting Deployment %s-collector, because of Deployment  is not needed", cluster.Name,
		))
		return r.deleteDeploymentIfOwnedByController(ctx, cluster)
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
) error {
	slurmDeployment, err := r.getDeployment(ctx, cluster)
	if err != nil {
		return errors.Wrap(err, "getting Deployment ")
	}
	// Check if the controller is the owner of the Deployment
	isOwner := isControllerOwnerDeployment(slurmDeployment, cluster)
	if !isOwner {
		// The controller is not the owner of the Deployment, nothing to do
		return nil
	}
	// The controller is the owner of the Deployment, delete it
	return r.deleteDeploymentOwnedByController(ctx, cluster, slurmDeployment)
}

func (r *DeploymentReconciler) getDeployment(ctx context.Context, cluster *slurmv1.SlurmCluster) (*appsv1.Deployment, error) {
	slurmDeployment := &appsv1.Deployment{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		slurmDeployment,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// slurmDeployment  doesn't exist, nothing to do
			return slurmDeployment, nil
		}
		// Other error occurred
		return nil, errors.Wrap(err, "getting Deployment ")
	}
	return slurmDeployment, nil
}

// Function to check if the controller is the owner
func isControllerOwnerDeployment(slurmDeployment *appsv1.Deployment, cluster *slurmv1.SlurmCluster) bool {
	// Check if the controller is the owner of the Deployment
	isOwner := false
	for _, ownerRef := range slurmDeployment.GetOwnerReferences() {
		if ownerRef.Kind == slurmv1.SlurmClusterKind && ownerRef.Name == cluster.Name {
			isOwner = true
			break
		}
	}

	return isOwner
}

func (r *DeploymentReconciler) deleteDeploymentOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	slurmDeployment *appsv1.Deployment,
) error {
	// Delete the Deployment
	err := r.Client.Delete(ctx, slurmDeployment)
	if err != nil {
		log.FromContext(ctx).
			WithValues("cluster", cluster.Name).
			Error(err, "Failed to delete Deployment ")
		return errors.Wrap(err, "deleting Deployment ")
	}
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
