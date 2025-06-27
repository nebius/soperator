package reconciler

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

type PodMonitorReconciler struct {
	*Reconciler
}

var (
	_ patcher = &PodMonitorReconciler{}
)

func NewPodMonitorReconciler(r *Reconciler) *PodMonitorReconciler {
	return &PodMonitorReconciler{
		Reconciler: r,
	}
}

func (r *PodMonitorReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired prometheusv1.PodMonitor,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx)
	if err := r.reconcile(ctx, cluster, &desired, r.patch, deps...); err != nil {
		logger.V(1).
			WithValues(logfield.ResourceKV(&desired)...).
			Error(err, "Failed to reconcile PodMonitor")
		return fmt.Errorf("reconciling PodMonitor: %w", err)
	}
	return nil
}

func (r *PodMonitorReconciler) Cleanup(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	resourceName string,
) error {
	logger := log.FromContext(ctx)

	podMonitor := &prometheusv1.PodMonitor{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      resourceName,
	}, podMonitor)

	if apierrors.IsNotFound(err) {
		logger.V(1).Info("PodMonitor not found, skipping deletion", "name", resourceName)
		return nil
	}

	if err != nil {
		return fmt.Errorf("getting PodMonitor %s: %w", resourceName, err)
	}

	if !metav1.IsControlledBy(podMonitor, cluster) {
		logger.V(1).Info("PodMonitor is not owned by controller, skipping deletion", "name", resourceName)
		return nil
	}

	if err := r.Delete(ctx, podMonitor); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("PodMonitor not found, skipping deletion", "name", resourceName)
			return nil
		}
		return fmt.Errorf("deleting PodMonitor %s: %w", resourceName, err)
	}

	logger.V(1).Info("PodMonitor deleted", "name", resourceName)
	return nil
}

func (r *PodMonitorReconciler) getPodMonitor(ctx context.Context, cluster *slurmv1.SlurmCluster) (*prometheusv1.PodMonitor, error) {
	podMonitor := &prometheusv1.PodMonitor{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		podMonitor,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// podMonitor doesn't exist, nothing to do
			return podMonitor, nil
		}
		// Other error occurred
		return nil, fmt.Errorf("getting PodMonitor: %w", err)
	}
	return podMonitor, nil
}

func (r *PodMonitorReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *prometheusv1.PodMonitor) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())
		dst.Spec.JobLabel = src.Spec.JobLabel
		dst.Spec.PodMetricsEndpoints = src.Spec.PodMetricsEndpoints
		return res
	}
	return patchImpl(existing.(*prometheusv1.PodMonitor), desired.(*prometheusv1.PodMonitor)), nil
}
