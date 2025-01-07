package reconciler

import (
	"context"
	"fmt"

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
		log.FromContext(ctx).Info(fmt.Sprintf("Deleting Role %s, because of Role is not needed", naming.BuildRoleWorkerName(cluster.Name)))
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

	if !metav1.IsControlledBy(role, cluster) {
		log.FromContext(ctx).Info("Role is not owned by controller, skipping deletion")
		return nil
	}

	if err := r.Delete(ctx, role); err != nil {
		if apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("Role not found, skipping deletion")
			return nil
		}
		return errors.Wrap(err, "deleting Worker Role")
	}
	log.FromContext(ctx).Info("Role deleted")
	return nil
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
			return nil, nil
		}
		return nil, err
	}
	return role, nil
}

func (r *RoleReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *rbacv1.Role) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		dst.Rules = append([]rbacv1.PolicyRule{}, src.Rules...)

		return res
	}
	return patchImpl(existing.(*rbacv1.Role), desired.(*rbacv1.Role)), nil
}
