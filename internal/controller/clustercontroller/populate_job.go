package clustercontroller

import (
	"context"
	"time"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/render/populate_jail"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// ReconcilePopulateJail reconciles and wait all resources necessary for deploying Populate Jail Job
func (r SlurmClusterReconciler) ReconcilePopulateJail(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	cluster *slurmv1.SlurmCluster,
) error {
	logger := log.FromContext(ctx)

	reconcilePopulateJailImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of Jail",
			utils.MultiStepExecutionStrategyFailAtFirstError,
			utils.MultiStepExecutionStep{
				Name: "Populate jail Job",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					isMaintenanceStopMode := check.IsModeDownscaleAndDeletePopulate(clusterValues.PopulateJail.Maintenance)
					desired := batchv1.Job{}
					getErr := r.Get(stepCtx,
						client.ObjectKey{
							Namespace: clusterValues.Namespace,
							Name:      clusterValues.PopulateJail.Name,
						},
						&desired,
					)
					if getErr == nil {
						stepLogger.Info("Already exists")
						if isMaintenanceStopMode {
							stepLogger.Info("Deleting")
							if err := r.Delete(stepCtx, &desired); err != nil {
								stepLogger.Error(err, "Failed to delete")
								return errors.Wrap(err, "deleting Populate jail Job")
							}
							stepLogger.Info("Deleted")
						}
						return nil
					}

					if !apierrors.IsNotFound(getErr) && !isMaintenanceStopMode {
						stepLogger.Error(getErr, "Failed to get")
						return errors.Wrap(getErr, "getting Populate jail Job")
					}

					if isMaintenanceStopMode {
						stepLogger.Info("Skipping creation due to MaintenanceStopMode")
						return nil
					}

					renderedDesired, err := populate_jail.RenderPopulateJailJob(
						clusterValues.Namespace,
						clusterValues.Name,
						clusterValues.ClusterType,
						clusterValues.NodeFilters,
						clusterValues.VolumeSources,
						&clusterValues.PopulateJail,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering Populate jail Job")
					}
					desired = *renderedDesired.DeepCopy()

					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err = r.Job.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling Populate jail Job")
					}
					stepLogger.Info("Reconciled")

					if pollErr := wait.PollUntilContextCancel(stepCtx,
						10*time.Second,
						true,
						func(pollCtx context.Context) (done bool, err error) {
							stepLogger.Info("Waiting")

							job := batchv1.Job{}
							if err = r.Get(stepCtx,
								client.ObjectKey{
									Namespace: clusterValues.Namespace,
									Name:      clusterValues.PopulateJail.Name,
								},
								&job,
							); err != nil {
								stepLogger.Error(err, "Failed to get")
								return false, errors.Wrap(err, "getting Populate jail Job")
							}
							stepLogger = stepLogger.WithValues(logfield.ResourceKV(&job)...)

							if job.Status.Succeeded > 0 {
								stepLogger.Info("Succeeded")
								return true, nil
							} else {
								stepLogger.Info("Not succeeded yet")
								return false, nil
							}
						},
					); pollErr != nil {
						stepLogger.Error(pollErr, "Failed to wait")
						return errors.Wrap(pollErr, "waiting Populate jail Job")
					}
					stepLogger.Info("Completed")

					return nil
				},
			},
		)
	}

	if err := reconcilePopulateJailImpl(); err != nil {
		logger.Error(err, "Failed to reconcile Populate jail Job")
		return errors.Wrap(err, "reconciling Populate jail Job")
	}
	logger.Info("Reconciled Populate jail Job")

	return nil
}
