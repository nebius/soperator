package reconciler

import (
	"context"
	"fmt"

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

type RoleBindingReconciler struct {
	*Reconciler
}

var (
	_ patcher = &RoleBindingReconciler{}
)

func NewRoleBindingReconciler(r *Reconciler) *RoleBindingReconciler {
	return &RoleBindingReconciler{
		Reconciler: r,
	}
}

func (r *RoleBindingReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *rbacv1.RoleBinding,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx)
	if desired == nil {
		return fmt.Errorf("desired RoleBinding cannot be nil")
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		logger.V(1).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile RoleBinding")
		return fmt.Errorf("reconciling RoleBinding: %w", err)
	}
	return nil
}

func (r *RoleBindingReconciler) Cleanup(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	resourceName string,
) error {
	logger := log.FromContext(ctx)

	roleBinding := &rbacv1.RoleBinding{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      resourceName,
	}, roleBinding)

	if apierrors.IsNotFound(err) {
		logger.V(1).Info("RoleBinding not found, skipping deletion", "name", resourceName)
		return nil
	}

	if err != nil {
		return fmt.Errorf("getting RoleBinding %s: %w", resourceName, err)
	}

	if !metav1.IsControlledBy(roleBinding, cluster) {
		logger.V(1).Info("RoleBinding is not owned by controller, skipping deletion", "name", resourceName)
		return nil
	}

	if err := r.Delete(ctx, roleBinding); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("RoleBinding not found, skipping deletion", "name", resourceName)
			return nil
		}
		return fmt.Errorf("deleting RoleBinding %s: %w", resourceName, err)
	}

	logger.V(1).Info("RoleBinding deleted", "name", resourceName)
	return nil
}

func (r *RoleBindingReconciler) getRoleBinding(ctx context.Context, cluster *slurmv1.SlurmCluster) (*rbacv1.RoleBinding, error) {
	roleBinding := &rbacv1.RoleBinding{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      naming.BuildRoleBindingWorkerName(cluster.Name),
		},
		roleBinding,
	)
	return roleBinding, err
}

func (r *RoleBindingReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *rbacv1.RoleBinding) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		dst.Subjects = append([]rbacv1.Subject{}, src.Subjects...)
		dst.RoleRef = src.RoleRef

		return res
	}
	return patchImpl(existing.(*rbacv1.RoleBinding), desired.(*rbacv1.RoleBinding)), nil
}
