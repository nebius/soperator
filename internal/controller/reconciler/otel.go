package reconciler

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

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
	if desired == nil {
		log.FromContext(ctx).Info(fmt.Sprintf("Deleting OpenTelemetryCollector %s-collector, because of OpenTelemetryCollector is not needed", cluster.Name))
		return r.deleteIfOwnedByController(ctx, cluster)
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile OpenTelemetryCollector")
		return errors.Wrap(err, "reconciling OpenTelemetryCollector")
	}
	return nil
}

func (r *OtelReconciler) deleteIfOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
) error {
	otel, err := r.getOtel(ctx, cluster)
	if apierrors.IsNotFound(err) {
		log.FromContext(ctx).Info("Service not found, skipping deletion")
		return nil
	}

	if err != nil {
		return errors.Wrap(err, "getting OpenTelemetryCollector")
	}

	if !metav1.IsControlledBy(otel, cluster) {
		log.FromContext(ctx).Info("Service is not owned by controller, skipping deletion")
		return nil
	}

	if err := r.Delete(ctx, otel); err != nil {
		return errors.Wrap(err, "deleting Service")
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
