package reconciler

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
	desired *prometheusv1.PodMonitor,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx)
	if desired == nil {
		// If desired is nil, delete the PodMonitor
		// TODO: Using error or desired is nil presence as an indicator for resource deletion doesn't seem good
		// We should use conditions instead. if condition is met and resource exists, delete it
		// MSP-2715 - task to improve resource deletion
		logger.V(1).Info(fmt.Sprintf(
			"Deleting PodMonitor %s-collector, because of PodMonitor is not needed", cluster.Name,
		))
		return r.deleteIfOwnedByController(ctx, cluster)
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		logger.V(1).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile PodMonitor")
		return errors.Wrap(err, "reconciling PodMonitor")
	}
	return nil
}

func (r *PodMonitorReconciler) deleteIfOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
) error {
	logger := log.FromContext(ctx)
	podMonitor, err := r.getPodMonitor(ctx, cluster)
	if apierrors.IsNotFound(err) {
		logger.V(1).Info("PodMonitor is not needed, skipping deletion")
		return nil
	}
	if err != nil {
		return errors.Wrap(err, "getting PodMonitor")
	}

	if !metav1.IsControlledBy(podMonitor, cluster) {
		logger.V(1).Info("PodMonitor is not owned by controller, skipping deletion")
		return nil
	}

	if err := r.Delete(ctx, podMonitor); err != nil {
		return errors.Wrap(err, "deleting PodMonitor")
	}
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
		return nil, errors.Wrap(err, "getting PodMonitor")
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
