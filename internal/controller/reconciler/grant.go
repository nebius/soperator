package reconciler

import (
	"context"

	"github.com/pkg/errors"

	mariadv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"

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
	desired *mariadv1alpha1.Grant,
	deps ...metav1.Object,
) error {
	if desired == nil {
		return errors.New("MariaDbGrant is not needed")
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile MariaDbGrant ")
		return errors.Wrap(err, "reconciling MariaDbGrant ")
	}
	return nil
}

func (r *MariaDbGrantReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *mariadv1alpha1.Grant) client.Patch {
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
	return patchImpl(existing.(*mariadv1alpha1.Grant), desired.(*mariadv1alpha1.Grant)), nil
}
