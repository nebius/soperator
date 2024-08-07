package reconciler

import (
	"context"

	"github.com/pkg/errors"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type RoleReconciler struct {
	*Reconciler
}

var (
	_ patcher = &RoleReconciler{}
)

func NewRoleReconciler(r *Reconciler) *RoleReconciler {
	return &RoleReconciler{
		Reconciler: r,
	}
}

func (r *RoleReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *rbacv1.Role,
	deps ...metav1.Object,
) error {
	if desired == nil {
		// If desired is nil, delete the Role
		return r.deleteRoleIfOwnedByController(ctx, cluster)
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile Worker Role")
		return errors.Wrap(err, "reconciling Worker Role")
	}
	return nil
}

func (r *RoleReconciler) deleteRoleIfOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
) error {
	role, err := r.getRole(ctx, cluster)
	if err != nil {
		return errors.Wrap(err, "getting Worker Role")
	}
	// Check if the controller is the owner of the Role
	isOwner := isControllerOwnerRole(role, cluster)
	if !isOwner {
		// The controller is not the owner of the Role, nothing to do
		return nil
	}
	// The controller is the owner of the Role, delete it
	return r.deleteRoleOwnedByController(ctx, cluster, role)
}

func (r *RoleReconciler) getRole(ctx context.Context, cluster *slurmv1.SlurmCluster) (*rbacv1.Role, error) {
	role := &rbacv1.Role{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      naming.BuildRoleWorkerName(cluster.Name),
		},
		role,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Role doesn't exist, nothing to do
			return role, nil
		}
		// Other error occurred
		return role, errors.Wrap(err, "getting Role")
	}
	return role, nil
}

// Function to check if the controller is the owner
func isControllerOwnerRole(role *rbacv1.Role, cluster *slurmv1.SlurmCluster) bool {
	// Check if the controller is the owner of the Role
	isOwner := false
	for _, ownerRef := range role.GetOwnerReferences() {
		if ownerRef.Kind == slurmv1.SlurmClusterKind && ownerRef.Name == cluster.Name {
			isOwner = true
			break
		}
	}

	return isOwner
}

func (r *RoleReconciler) deleteRoleOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	role *rbacv1.Role,
) error {
	// Delete the Role
	err := r.Client.Delete(ctx, role)
	if err != nil {
		log.FromContext(ctx).
			WithValues("cluster", cluster.Name).
			Error(err, "Failed to delete Worker Role")
		return errors.Wrap(err, "deleting Worker  Role")
	}
	return nil
}

func (r *RoleReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *rbacv1.Role) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		dst.Rules = append([]rbacv1.PolicyRule{}, src.Rules...)

		return res
	}
	return patchImpl(existing.(*rbacv1.Role), desired.(*rbacv1.Role)), nil
}
