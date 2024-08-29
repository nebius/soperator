package reconciler

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type SlurmExporterReconciler struct {
	*Reconciler
}

var (
	_ patcher = &SlurmExporterReconciler{}
)

func NewSlurmExporterReconciler(r *Reconciler) *SlurmExporterReconciler {
	return &SlurmExporterReconciler{
		Reconciler: r,
	}
}

func (r *SlurmExporterReconciler) Reconcile(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	desired *appsv1.Deployment,
	deps ...metav1.Object,
) error {
	if desired == nil {
		// If desired is nil, delete the Exporter
		log.FromContext(ctx).Info(fmt.Sprintf(
			"Deleting Exporter %s-collector, because of Slurm Exporter is not needed", cluster.Name,
		))
		return r.deleteExporterIfOwnedByController(ctx, cluster)
	}
	if err := r.reconcile(ctx, cluster, desired, r.patch, deps...); err != nil {
		log.FromContext(ctx).
			WithValues(logfield.ResourceKV(desired)...).
			Error(err, "Failed to reconcile Slurm Exporter")
		return errors.Wrap(err, "reconciling Slurm Exporter")
	}
	return nil
}

func (r *SlurmExporterReconciler) deleteExporterIfOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
) error {
	slurmExporter, err := r.getExporter(ctx, cluster)
	if err != nil {
		return errors.Wrap(err, "getting Slurm Exporter")
	}
	// Check if the controller is the owner of the Exporter
	isOwner := isControllerOwnerExporter(slurmExporter, cluster)
	if !isOwner {
		// The controller is not the owner of the Exporter, nothing to do
		return nil
	}
	// The controller is the owner of the Exporter, delete it
	return r.deleteExporterOwnedByController(ctx, cluster, slurmExporter)
}

func (r *SlurmExporterReconciler) getExporter(ctx context.Context, cluster *slurmv1.SlurmCluster) (*appsv1.Deployment, error) {
	slurmExporter := &appsv1.Deployment{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		slurmExporter,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// slurmExporter doesn't exist, nothing to do
			return slurmExporter, nil
		}
		// Other error occurred
		return nil, errors.Wrap(err, "getting Slurm Exporter")
	}
	return slurmExporter, nil
}

// Function to check if the controller is the owner
func isControllerOwnerExporter(slurmExporter *appsv1.Deployment, cluster *slurmv1.SlurmCluster) bool {
	// Check if the controller is the owner of the Role
	isOwner := false
	for _, ownerRef := range slurmExporter.GetOwnerReferences() {
		if ownerRef.Kind == slurmv1.SlurmClusterKind && ownerRef.Name == cluster.Name {
			isOwner = true
			break
		}
	}

	return isOwner
}

func (r *SlurmExporterReconciler) deleteExporterOwnedByController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	slurmExporter *appsv1.Deployment,
) error {
	// Delete the Role
	err := r.Client.Delete(ctx, slurmExporter)
	if err != nil {
		log.FromContext(ctx).
			WithValues("cluster", cluster.Name).
			Error(err, "Failed to delete Slurm Exporter")
		return errors.Wrap(err, "deleting Slurm Exporter")
	}
	return nil
}

func (r *SlurmExporterReconciler) patch(existing, desired client.Object) (client.Patch, error) {
	patchImpl := func(dst, src *appsv1.Deployment) client.Patch {
		res := client.MergeFrom(dst.DeepCopy())
		dst.Spec.Replicas = src.Spec.Replicas
		dst.Spec.Strategy = src.Spec.Strategy
		dst.Spec.Selector = src.Spec.Selector
		dst.Spec.Template.Spec = src.Spec.Template.Spec
		return res
	}
	return patchImpl(existing.(*appsv1.Deployment), desired.(*appsv1.Deployment)), nil
}
