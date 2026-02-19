package clustercontroller

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
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
					if !isMaintenanceStopMode {
						hasActivePods, err := hasNonTerminalLoginOrWorkerPods(
							stepCtx,
							r.Client,
							clusterValues.Namespace,
							clusterValues.Name,
						)
						if err != nil {
							stepLogger.Error(err, "Failed to check running login/worker pods")
							return fmt.Errorf("checking running login/worker pods: %w", err)
						}
						if hasActivePods {
							stepLogger.Info("Skipping Populate jail Job: login or worker pods are not fully terminated")
							return nil
						}
					}

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
								return fmt.Errorf("deleting Populate jail Job: %w", err)
							}
							stepLogger.V(1).Info("Deleted")
							return nil
						}
						if check.IsModeDownscaleAndOverwritePopulate(clusterValues.PopulateJail.Maintenance) {
							if isConditionNonOverwrite(cluster.Status.Conditions) {
								if err := r.Delete(stepCtx, &desired); err != nil {
									stepLogger.Error(err, "Failed to delete")
									return fmt.Errorf("deleting Populate jail Job: %w", err)
								}
								stepLogger.V(1).Info("Successfully deleted Populate Jail Job")
							}
							return nil
						}
					}

					if getErr != nil && !apierrors.IsNotFound(getErr) && !isMaintenanceStopMode {
						stepLogger.Error(getErr, "Failed to get")
						return fmt.Errorf("getting Populate jail Job: %w", getErr)
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
						return fmt.Errorf("reconciling Populate jail Job: %w", err)
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
								return false, fmt.Errorf("getting Populate jail Job: %w", err)
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
						return fmt.Errorf("waiting Populate jail Job: %w", pollErr)
					}
					stepLogger.V(1).Info("Completed")

					return nil
				},
			},
		)
	}

	if err := reconcilePopulateJailImpl(); err != nil {
		logger.Error(err, "Failed to reconcile Populate jail Job")
		return fmt.Errorf("reconciling Populate jail Job: %w", err)
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

func hasNonTerminalLoginOrWorkerPods(
	ctx context.Context,
	cl client.Client,
	namespace string,
	clusterName string,
) (bool, error) {
	loginPods := &corev1.PodList{}
	if err := cl.List(
		ctx,
		loginPods,
		client.InNamespace(namespace),
		client.MatchingLabels{
			consts.LabelInstanceKey:  clusterName,
			consts.LabelComponentKey: consts.ComponentTypeLogin.String(),
		},
	); err != nil {
		return false, fmt.Errorf("listing login pods: %w", err)
	}
	if hasNonTerminalPods(loginPods.Items) {
		return true, nil
	}

	workerPods := &corev1.PodList{}
	if err := cl.List(
		ctx,
		workerPods,
		client.InNamespace(namespace),
		client.MatchingLabels{
			consts.LabelInstanceKey: clusterName,
			consts.LabelWorkerKey:   consts.LabelWorkerValue,
		},
	); err != nil {
		return false, fmt.Errorf("listing worker pods: %w", err)
	}
	return hasNonTerminalPods(workerPods.Items), nil
}

func hasNonTerminalPods(pods []corev1.Pod) bool {
	for _, pod := range pods {
		if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
			return true
		}
	}
	return false
}
