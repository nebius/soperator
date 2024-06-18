package clustercontroller

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/render/populate_jail"
	"nebius.ai/slurm-operator/internal/values"
)

// DeployPopulateJail creates populate job resources for Slurm cluster
func (r SlurmClusterReconciler) DeployPopulateJail(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	clusterCR *slurmv1.SlurmCluster,
) (ctrl.Result, batchv1.Job, error) {
	logger := log.FromContext(ctx)

	found := &batchv1.Job{}
	dep, err := populate_jail.RenderJob(
		clusterValues.Namespace,
		clusterValues.Name,
		clusterValues.NodeFilters,
		clusterValues.VolumeSources,
		&clusterValues.PopulateJail,
	)
	if err != nil {
		logger.Error(err, "PopulateJail Job deployment failed")
		return ctrl.Result{}, batchv1.Job{}, err
	}

	if res, err := r.EnsureDeployed(ctx, &dep, found, clusterCR, []v1.Object{}...); err != nil {
		return res, batchv1.Job{}, err
	}

	return ctrl.Result{}, dep, nil
}

func (r SlurmClusterReconciler) CheckPopulateJail(
	ctx context.Context,
	job *batchv1.Job,
) (ctrl.Result, bool, error) {
	logger := log.FromContext(ctx)

	err := r.Get(ctx, client.ObjectKey{Name: job.Name, Namespace: job.Namespace}, job)
	if err != nil && apierrors.IsNotFound(err) {
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, true, err
	} else if err != nil {
		logger.Error(err, "Failed to get PopulateJail Job")
		return ctrl.Result{}, true, err
	}

	if job.Status.Succeeded > 0 {
		logger.Info("PopulateJail Job completed successfully")
		return ctrl.Result{}, false, nil
	}

	logger.Info("PopulateJail Job status not completed yet")
	return ctrl.Result{}, true, nil
}
