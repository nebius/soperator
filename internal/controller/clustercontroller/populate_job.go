package clustercontroller

import (
	"context"
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/render/populate_jail"
	"nebius.ai/slurm-operator/internal/values"
)

// ReconcilePopulateJail reconciles and wait all resources necessary for deploying Populate Jail Job
func (r SlurmClusterReconciler) ReconcilePopulateJail(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	cluster *slurmv1.SlurmCluster,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	job, err := populate_jail.RenderPopulateJailJob(
		clusterValues.Namespace,
		clusterValues.Name,
		clusterValues.NodeFilters,
		clusterValues.VolumeSources,
		&clusterValues.PopulateJail,
	)
	if err != nil {
		logger.Error(err, "Failed to render Populate Jail Job")
		return ctrl.Result{}, errors.Wrap(err, "rendering Populate Jail Job")
	}

	reconcilePopulateJailImpl := func() error {
		logger = logger.WithValues(logfield.ResourceKV(&job)...)

		err = r.Job.Reconcile(ctx, cluster, &job, []v1.Object{}...)
		if err != nil {
			return err
		}
		return nil
	}

	err = r.Get(ctx, client.ObjectKey{Name: job.Name, Namespace: job.Namespace}, job.DeepCopy())
	if err != nil && apierrors.IsNotFound(err) {
		if err := reconcilePopulateJailImpl(); err != nil {
			logger.Error(err, "Failed to reconcile Populate Jail Job")
			return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, errors.Wrap(err, "reconciling Populate Jail Job")
		}
	} else if err != nil {
		logger.Error(err, "Failed to get PopulateJail Job")
		return ctrl.Result{}, errors.Wrap(err, "reconciling Populate Jail Job")
	}

	if job.Status.Succeeded > 0 {
		logger.Info("PopulateJail Job completed successfully")
		return ctrl.Result{}, nil
	}

	logger.Info("PopulateJail Job status not completed yet")
	return ctrl.Result{}, nil
}
