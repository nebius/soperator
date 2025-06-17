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

	otelv1beta1 "github.com/open-telemetry/opentelemetry-operator/apis/v1beta1"
)

type OtelReconciler struct {
	*Reconciler
}

var (
	_ patcher = &OtelReconciler{}
)

func NewOtelReconciler(r *Reconciler) *OtelReconciler {
	return &OtelReconciler{
		Reconciler: r,
	}
}

func (r *OtelReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *otelv1beta1.OpenTelemetryCollector,
	deps ...metav1.Object,
) error {
	logger := log.FromContext(ctx)
	if desired == nil {
		logger.V(1).Info(fmt.Sprintf("Deleting OpenTelemetryCollector %s-collector, because of OpenTelemetryCollector is not needed", cluster.Name))
		return r.deleteIfOwnedByController(ctx, cluster)
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile OpenTelemetryCollector")
		return fmt.Errorf("reconciling OpenTelemetryCollector: %w", err)
	}
	return nil
}

func (r *OtelReconciler) deleteIfOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
) error {
	logger := log.FromContext(ctx)
	otel, err := r.getOtel(ctx, cluster)
	if apierrors.IsNotFound(err) {
		logger.V(1).Info("Service not found, skipping deletion")
		return nil
	}

	if err != nil {
		return fmt.Errorf("getting OpenTelemetryCollector: %w", err)
	}

	if !metav1.IsControlledBy(otel, cluster) {
		logger.V(1).Info("Service is not owned by controller, skipping deletion")
		return nil
	}

	if err := r.Delete(ctx, otel); err != nil {
		return fmt.Errorf("deleting Service: %w", err)
	}

	return nil
}

func (r *OtelReconciler) getOtel(ctx context.Context, cluster *slurmv1.SlurmCluster) (*otelv1beta1.OpenTelemetryCollector, error) {
	otel := &otelv1beta1.OpenTelemetryCollector{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		otel,
	)
	return otel, err
}

func (r *OtelReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *otelv1beta1.OpenTelemetryCollector) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())
		dst.Spec = src.Spec
		return res
	}
	return patchImpl(existing.(*otelv1beta1.OpenTelemetryCollector), desired.(*otelv1beta1.OpenTelemetryCollector)), nil
}
