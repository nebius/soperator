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

type MariaDbReconciler struct {
	*Reconciler
}

var (
	_ patcher = &MariaDbReconciler{}
)

func NewMariaDbReconciler(r *Reconciler) *MariaDbReconciler {
	return &MariaDbReconciler{
		Reconciler: r,
	}
}

func (r *MariaDbReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *mariadv1alpha1.MariaDB,
	deps ...metav1.Object,
) error {
	if desired == nil {
		return errors.New("MariaDb is not needed")
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile MariaDb ")
		return errors.Wrap(err, "reconciling MariaDb ")
	}
	return nil
}

func (r *MariaDbReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *mariadv1alpha1.MariaDB) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())
		dst.Spec.Image = src.Spec.Image
		dst.Spec.Replicas = src.Spec.Replicas
		dst.Spec.Port = src.Spec.Port
		dst.Spec.Storage = src.Spec.Storage
		dst.Spec.Database = src.Spec.Database
		dst.Spec.Username = src.Spec.Username
		dst.Spec.PasswordSecretKeyRef = src.Spec.PasswordSecretKeyRef
		dst.Spec.RootEmptyPassword = src.Spec.RootEmptyPassword
		dst.Spec.Service = src.Spec.Service
		dst.Spec.Affinity = src.Spec.Affinity
		dst.Spec.Tolerations = src.Spec.Tolerations
		dst.Spec.NodeSelector = src.Spec.NodeSelector
		dst.Spec.Resources = src.Spec.Resources
		dst.Spec.Metrics = src.Spec.Metrics
		return res
	}
	return patchImpl(existing.(*mariadv1alpha1.MariaDB), desired.(*mariadv1alpha1.MariaDB)), nil
}
