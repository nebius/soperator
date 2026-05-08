package reconciler

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

type ServiceMonitorReconciler struct {
	*Reconciler
}

var (
	_ patcher = &ServiceMonitorReconciler{}
)

func NewServiceMonitorReconciler(r *Reconciler) *ServiceMonitorReconciler {
	return &ServiceMonitorReconciler{
		Reconciler: r,
	}
}

func (r *ServiceMonitorReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired prometheusv1.ServiceMonitor,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx)
	if err := r.reconcile(ctx, cluster, &desired, r.patch, deps...); err != nil {
		logger.V(1).
			WithValues(logfield.ResourceKV(&desired)...).
			Error(err, "Failed to reconcile ServiceMonitor")
		return fmt.Errorf("reconciling ServiceMonitor: %w", err)
	}
	return nil
}

func (r *ServiceMonitorReconciler) Cleanup(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	resourceName string,
) error {
	logger := log.FromContext(ctx)

	serviceMonitor := &prometheusv1.ServiceMonitor{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      resourceName,
	}, serviceMonitor)

	if apierrors.IsNotFound(err) {
		logger.V(1).Info("ServiceMonitor not found, skipping deletion", "name", resourceName)
		return nil
	}

	if err != nil {
		return fmt.Errorf("getting ServiceMonitor %s: %w", resourceName, err)
	}

	if !metav1.IsControlledBy(serviceMonitor, cluster) {
		logger.V(1).Info("ServiceMonitor is not owned by controller, skipping deletion", "name", resourceName)
		return nil
	}

	if err := r.Delete(ctx, serviceMonitor); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("ServiceMonitor not found, skipping deletion", "name", resourceName)
			return nil
		}
		return fmt.Errorf("deleting ServiceMonitor %s: %w", resourceName, err)
	}

	logger.V(1).Info("ServiceMonitor deleted", "name", resourceName)
	return nil
}

func (r *ServiceMonitorReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *prometheusv1.ServiceMonitor) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())
		dst.Spec.JobLabel = src.Spec.JobLabel
		dst.Spec.Endpoints = src.Spec.Endpoints
		dst.Spec.Selector = src.Spec.Selector
		dst.Spec.NamespaceSelector = src.Spec.NamespaceSelector
		return res
	}
	return patchImpl(existing.(*prometheusv1.ServiceMonitor), desired.(*prometheusv1.ServiceMonitor)), nil
}
