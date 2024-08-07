package reconciler

import (
	"context"

	"github.com/pkg/errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
	otelINtaled bool,
	deps ...metav1.Object,
) error {
	if !otelINtaled {
		// If desired is nil, delete the Role
		log.FromContext(ctx).Info("Deleting OpenTelemetryCollector")
		return r.deleteOteIfOwnedByController(ctx, cluster)
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile OpenTelemetryCollector")
		return errors.Wrap(err, "reconciling OpenTelemetryCollector")
	}
	return nil
}

func (r *OtelReconciler) deleteOteIfOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
) error {
	otel, err := r.getOtel(ctx, cluster)
	if err != nil {
		return errors.Wrap(err, "getting OpenTelemetryCollector")
	}
	// Check if the controller is the owner of the OpenTelemetryCollector
	isOwner := isControllerOwnerOtel(otel, cluster)
	if !isOwner {
		// The controller is not the owner of the OpenTelemetryCollector, nothing to do
		return nil
	}
	// The controller is the owner of the OpenTelemetryCollector, delete it
	return r.deleteOpenTelemetryCollectorOwnedByController(ctx, cluster, otel)
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
	if err != nil {
		if apierrors.IsNotFound(err) {
			// otel doesn't exist, nothing to do
			return otel, nil
		}
		// Other error occurred
		return otel, errors.Wrap(err, "getting Worker OpenTelemetryCollector")
	}
	return otel, nil
}

// Function to check if the controller is the owner
func isControllerOwnerOtel(otel *otelv1beta1.OpenTelemetryCollector, cluster *slurmv1.SlurmCluster) bool {
	// Check if the controller is the owner of the Role
	isOwner := false
	for _, ownerRef := range otel.GetOwnerReferences() {
		if ownerRef.Kind == slurmv1.SlurmClusterKind && ownerRef.Name == cluster.Name {
			isOwner = true
			break
		}
	}

	return isOwner
}

func (r *OtelReconciler) deleteOpenTelemetryCollectorOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	otel *otelv1beta1.OpenTelemetryCollector,
) error {
	// Delete the Role
	err := r.Client.Delete(ctx, otel)
	if err != nil {
		log.FromContext(ctx).
			WithValues("cluster", cluster.Name).
			Error(err, "Failed to delete Worker OpenTelemetryCollector")
		return errors.Wrap(err, "deleting Worker OpenTelemetryCollector")
	}
	return nil
}

func (r *OtelReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *otelv1beta1.OpenTelemetryCollector) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())
		dst.Spec = src.Spec
		return res
	}
	return patchImpl(existing.(*otelv1beta1.OpenTelemetryCollector), desired.(*otelv1beta1.OpenTelemetryCollector)), nil
}
