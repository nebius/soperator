package reconciler

import (
	"context"

	"github.com/pkg/errors"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type MariaDbGrantReconciler struct {
	*Reconciler
}

var (
	_ patcher = &MariaDbGrantReconciler{}
)

func NewMariaDbGrantReconciler(r *Reconciler) *MariaDbGrantReconciler {
	return &MariaDbGrantReconciler{
		Reconciler: r,
	}
}

func (r *MariaDbGrantReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *mariadbv1alpha1.Grant,
	name *string,
	deps ...metav1.Object,
) error {
	if desired == nil {
		if name == nil {
			log.FromContext(ctx).Info("MariaDbGrant is not needed, skipping deletion")
			return nil
		}
		log.FromContext(ctx).Info("Deleting MariaDbGrant, because MariaDbGrant is not needed")
		return r.deleteIfOwnedByController(ctx, cluster, cluster.Namespace, *name)
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile MariaDbGrant ")
		return errors.Wrap(err, "reconciling MariaDbGrant ")
	}
	return nil
}

func (r *MariaDbGrantReconciler) deleteIfOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	namespace,
	name string,
) error {
	grant, err := r.getMariaDbGrant(ctx, namespace, name)
	if err != nil {
		return errors.Wrap(err, "getting MariaDbGrant")
	}

	if !metav1.IsControlledBy(grant, cluster) {
		log.FromContext(ctx).Info("MariaDbGrant is not owned by controller, skipping deletion")
		return nil
	}

	if err := r.Client.Delete(ctx, grant); err != nil {
		return errors.Wrap(err, "deleting MariaDbGrant")
	}
	return nil
}

func (r *MariaDbGrantReconciler) getMariaDbGrant(
	ctx context.Context,
	namespace,
	name string,
) (*mariadbv1alpha1.Grant, error) {
	grant := &mariadbv1alpha1.Grant{}
	err := r.Client.Get(
		ctx,
		client.ObjectKey{
			Namespace: namespace,
			Name:      name,
		},
		grant,
	)
	return grant, err
}

func (r *MariaDbGrantReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *mariadbv1alpha1.Grant) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())
		dst.Spec.Username = src.Spec.Username
		dst.Spec.Host = src.Spec.Host
		dst.Spec.Database = src.Spec.Database
		dst.Spec.Table = src.Spec.Table
		dst.Spec.Privileges = src.Spec.Privileges
		dst.Spec.GrantOption = src.Spec.GrantOption
		dst.Spec.MariaDBRef = src.Spec.MariaDBRef
		return res
	}
	return patchImpl(existing.(*mariadbv1alpha1.Grant), desired.(*mariadbv1alpha1.Grant)), nil
}
