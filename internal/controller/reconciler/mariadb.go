package reconciler

import (
	"context"

	"github.com/pkg/errors"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	desired *mariadbv1alpha1.MariaDB,
	name *string,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx)
	if desired == nil {
		// If desired is nil, delete the MariaDb
		if name == nil {
			logger.V(1).Info("MariaDb is not needed, skipping deletion")
			return nil
		}
		logger.V(1).Info("Deleting MariaDb, because MariaDb is not needed")
		return r.deleteIfOwnedByController(ctx, cluster, cluster.Namespace, *name)
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		logger.V(1).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile MariaDb ")
		return errors.Wrap(err, "reconciling MariaDb ")
	}
	return nil
}

func (r *MariaDbReconciler) deleteIfOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	namespace,
	name string,
) error {
	logger := log.FromContext(ctx)
	mariaDb, err := r.getMariaDb(ctx, namespace, name)
	if apierrors.IsNotFound(err) {
		logger.V(1).Info("MariaDb is not found, skipping deletion")
		return nil
	}
	if err != nil {
		return errors.Wrap(err, "getting MariaDb")
	}

	if !metav1.IsControlledBy(mariaDb, cluster) {
		logger.V(1).Info("MariaDb is not owned by controller, skipping deletion")
		return nil
	}

	return r.Client.Delete(ctx, mariaDb)
}

func (r *MariaDbReconciler) getMariaDb(ctx context.Context, namespace, name string) (*mariadbv1alpha1.MariaDB, error) {
	mariaDb := &mariadbv1alpha1.MariaDB{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, mariaDb)
	return mariaDb, err
}

func (r *MariaDbReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *mariadbv1alpha1.MariaDB) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())
		dst.Spec.Image = src.Spec.Image
		dst.Spec.Replicas = src.Spec.Replicas
		dst.Spec.Port = src.Spec.Port
		dst.Spec.Database = src.Spec.Database
		dst.Spec.Username = src.Spec.Username
		dst.Spec.PasswordSecretKeyRef = src.Spec.PasswordSecretKeyRef
		dst.Spec.RootEmptyPassword = src.Spec.RootEmptyPassword
		dst.Spec.Service = src.Spec.Service
		dst.Spec.Affinity = src.Spec.Affinity
		dst.Spec.Tolerations = src.Spec.Tolerations
		dst.Spec.NodeSelector = src.Spec.NodeSelector
		dst.Spec.Resources = src.Spec.Resources
		dst.Spec.SecurityContext = src.Spec.SecurityContext
		dst.Spec.PodSecurityContext = src.Spec.PodSecurityContext
		dst.Spec.Storage.Ephemeral = src.Spec.Storage.Ephemeral
		dst.Spec.Storage.StorageClassName = src.Spec.Storage.StorageClassName
		dst.Spec.Storage.VolumeClaimTemplate = src.Spec.Storage.VolumeClaimTemplate
		dst.Spec.Storage.Size = src.Spec.Storage.Size

		return res
	}
	return patchImpl(existing.(*mariadbv1alpha1.MariaDB), desired.(*mariadbv1alpha1.MariaDB)), nil
}
