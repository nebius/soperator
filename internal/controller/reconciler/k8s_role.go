package reconciler

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/logfield"
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
	owner client.Object,
	desired rbacv1.Role,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx)
	if err := r.reconcile(ctx, owner, &desired, r.patch, deps...); err != nil {
		logger.V(1).
			WithValues(logfield.ResourceKV(&desired)...).
			Error(err, "Failed to reconcile Worker Role")
		return fmt.Errorf("reconciling Worker Role: %w", err)
	}
	return nil
}

func (r *RoleReconciler) Cleanup(
	ctx context.Context,
	owner client.Object,
	roleName string,
) error {
	logger := log.FromContext(ctx)

	role := &rbacv1.Role{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: owner.GetNamespace(),
		Name:      roleName,
	}, role)

	if apierrors.IsNotFound(err) {
		logger.V(1).Info("Role not found, skipping deletion", "name", roleName)
		return nil
	}

	if err != nil {
		return fmt.Errorf("getting Role %s: %w", roleName, err)
	}

	if !metav1.IsControlledBy(role, owner) {
		logger.V(1).Info("Role is not owned by controller, skipping deletion", "name", roleName)
		return nil
	}

	if err := r.Delete(ctx, role); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("Role not found, skipping deletion", "name", roleName)
			return nil
		}
		return fmt.Errorf("deleting Role %s: %w", roleName, err)
	}

	logger.V(1).Info("Role deleted", "name", roleName)
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
