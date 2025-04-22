package clustercontroller

import (
	"context"
	"time"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
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
					stepLogger.V(1).Info("Reconciling")

					isMaintenanceStopMode := check.IsModeDownscaleAndDeletePopulate(
						clusterValues.PopulateJail.Maintenance)

					desired := batchv1.Job{}
					getErr := r.Get(stepCtx,
						client.ObjectKey{
							Namespace: clusterValues.Namespace,
							Name:      clusterValues.PopulateJail.Name,
						},
						&desired,
					)
					if getErr == nil {
						stepLogger.V(1).Info("Already exists")
						if isMaintenanceStopMode {
							stepLogger.V(1).Info("Deleting")
							if err := r.Delete(stepCtx, &desired); err != nil {
								stepLogger.Error(err, "Failed to delete")
								return errors.Wrap(err, "deleting Populate jail Job")
							}
							stepLogger.V(1).Info("Deleted")
							return nil
						}
						if check.IsModeDownscaleAndOverwritePopulate(clusterValues.PopulateJail.Maintenance) {
							if isConditionNonOverwrite(cluster.Status.Conditions) {
								if err := r.Delete(stepCtx, &desired); err != nil {
									stepLogger.Error(err, "Failed to delete")
									return errors.Wrap(err, "deleting Populate jail Job")
								}
								stepLogger.V(1).Info("Successfully deleted Populate Jail Job")
							}
							return nil
						}
					}

					if getErr != nil && !apierrors.IsNotFound(getErr) && !isMaintenanceStopMode {
						stepLogger.Error(getErr, "Failed to get")
						return errors.Wrap(getErr, "getting Populate jail Job")
					}

					if isMaintenanceStopMode {
						stepLogger.V(1).Info("Skipping creation due to MaintenanceStopMode")
						return nil
					}

					renderedDesired := populate_jail.RenderPopulateJailJob(
						clusterValues.Namespace,
						clusterValues.Name,
						clusterValues.ClusterType,
						clusterValues.NodeFilters,
						clusterValues.VolumeSources,
						&clusterValues.PopulateJail,
					)
					desired = *renderedDesired.DeepCopy()

					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.Job.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling Populate jail Job")
					}
					stepLogger.V(1).Info("Reconciled")

					if pollErr := wait.PollUntilContextCancel(stepCtx,
						10*time.Second,
						true,
						func(pollCtx context.Context) (done bool, err error) {
							stepLogger.V(1).Info("Waiting")

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
								stepLogger.V(1).Info("Succeeded")
								return true, nil
							} else {
								stepLogger.V(1).Info("Not succeeded yet")
								return false, nil
							}
						},
					); pollErr != nil {
						stepLogger.Error(pollErr, "Failed to wait")
						return errors.Wrap(pollErr, "waiting Populate jail Job")
					}
					stepLogger.V(1).Info("Completed")

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

func isConditionNonOverwrite(conditions []metav1.Condition) bool {
	for _, condition := range conditions {
		if condition.Type == slurmv1.ConditionClusterPopulateJailMode {
			return condition.Reason != string(consts.ModeDownscaleAndOverwritePopulate)
		}
	}
	return false
}
