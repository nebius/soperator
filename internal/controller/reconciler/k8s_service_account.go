package reconciler

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
)

type ServiceAccountReconciler struct {
	*Reconciler
}

var (
	_ patcher = &ServiceAccountReconciler{}
)

func NewServiceAccountReconciler(r *Reconciler) *ServiceAccountReconciler {
	return &ServiceAccountReconciler{
		Reconciler: r,
	}
}

func (r *ServiceAccountReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *corev1.ServiceAccount,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx)
	if desired == nil {
		return fmt.Errorf("desired ServiceAccount cannot be nil")
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		logger.V(1).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile ServiceAccount")
		return fmt.Errorf("reconciling ServiceAccount: %w", err)
	}
	return nil
}

func (r *ServiceAccountReconciler) Cleanup(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	serviceAccountName string,
) error {
	logger := log.FromContext(ctx)

	serviceAccount := &corev1.ServiceAccount{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      serviceAccountName,
	}, serviceAccount)

	if apierrors.IsNotFound(err) {
		logger.V(1).Info("ServiceAccount not found, skipping deletion", "name", serviceAccountName)
		return nil
	}

	if err != nil {
		return fmt.Errorf("getting ServiceAccount %s: %w", serviceAccountName, err)
	}

	if !metav1.IsControlledBy(serviceAccount, cluster) {
		logger.V(1).Info("ServiceAccount is not owned by controller, skipping deletion", "name", serviceAccountName)
		return nil
	}

	if err := r.Delete(ctx, serviceAccount); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("ServiceAccount not found, skipping deletion", "name", serviceAccountName)
			return nil
		}
		return fmt.Errorf("deleting ServiceAccount %s: %w", serviceAccountName, err)
	}

	logger.V(1).Info("ServiceAccount deleted", "name", serviceAccountName)
	return nil
}

func (r *ServiceAccountReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *corev1.ServiceAccount) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())

		dst.Name = src.Name

		return res
	}
	return patchImpl(existing.(*corev1.ServiceAccount), desired.(*corev1.ServiceAccount)), nil
}
