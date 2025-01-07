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
	if desired == nil {
		// If desired is nil, delete the Role Binding
		log.FromContext(ctx).Info(fmt.Sprintf("Deleting RoleBinding %s, because of RoleBinding is not needed", naming.BuildRoleBindingWorkerName(cluster.Name)))
		return r.deleteRoleBindingIfOwnedByController(ctx, cluster)
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile RoleBinding")
		return errors.Wrap(err, "reconciling RoleBinding")
	}
	return nil
}

func (r *RoleBindingReconciler) deleteRoleBindingIfOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
) error {
	roleBinding, err := r.getRoleBinding(ctx, cluster)
	if err != nil {
		return errors.Wrap(err, "getting Worker RoleBinding")
	}

	if !metav1.IsControlledBy(roleBinding, cluster) {
		log.FromContext(ctx).Info("RoleBinding is not owned by controller, skipping deletion")
		return nil
	}

	if err := r.Delete(ctx, roleBinding); err != nil {
		if apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("RoleBinding is already deleted")
			return nil
		}
		return errors.Wrap(err, "deleting RoleBinding")
	}

	log.FromContext(ctx).Info("RoleBinding deleted")
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
