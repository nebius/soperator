package clustercontroller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/rest"
	"nebius.ai/slurm-operator/internal/render/sconfigcontroller"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

func (r SlurmClusterReconciler) ReconcileSConfigController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) error {
	logger := log.FromContext(ctx)

	reconcileSConfigControllerImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of SConfigController",
			utils.MultiStepExecutionStrategyCollectErrors,
			utils.MultiStepExecutionStep{
				Name: "SConfigController Deployment",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling SConfigController Deployment")

					desired, err := sconfigcontroller.RenderDeployment(
						clusterValues.Namespace,
						clusterValues.Name,
						rest.GetServiceURL(clusterValues.Namespace, &clusterValues.NodeRest),
						&clusterValues.SConfigController,
						clusterValues.NodeFilters,
						clusterValues.VolumeSources,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return fmt.Errorf("rendering SConfigController Deployment: %w", err)
					}

					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.V(1).Info("Rendered")

					if err = r.Deployment.Reconcile(stepCtx, cluster, *desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling SConfigController Deployment: %w", err)
					}

					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},
		)
	}

	if err := reconcileSConfigControllerImpl(); err != nil {
		logger.Error(err, "Failed to reconcile SConfigController")
		return fmt.Errorf("reconciling SConfigController: %w", err)
	}
	logger.V(1).Info("Reconciled SConfigController")
	return nil
}

func (r SlurmClusterReconciler) ValidateSConfigController(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) (ctrl.Result, error) {
	const requeueDuration = 10 * time.Second
	var (
		res = ctrl.Result{}
	)

	logger := log.FromContext(ctx)

	existing := &appsv1.Deployment{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      naming.BuildDeploymentName(consts.ComponentTypeSConfigController),
		},
		existing,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: requeueDuration}, nil
		}
		logger.Error(err, "Failed to get SConfigController Deployment")
		return res, fmt.Errorf("getting SConfigController Deployment: %w", err)
	}

	if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) bool {
		var (
			changesInStatus     = false
			changesInConditions = false
		)

		if status.ReadySConfigController == nil {
			status.ReadySConfigController = ptr.To(int32(0))
		}
		if *status.ReadySConfigController != existing.Status.AvailableReplicas {
			status.ReadySConfigController = &existing.Status.AvailableReplicas
			changesInStatus = true
		}

		var (
			condition metav1.Condition
		)
		if existing.Status.AvailableReplicas == clusterValues.NodeLogin.StatefulSet.Replicas {
			condition = metav1.Condition{
				Type:    slurmv1.ConditionClusterSConfigControllerAvailable,
				Status:  metav1.ConditionTrue,
				Reason:  "Available",
				Message: "Slurm SConfigController is available",
			}
		} else {
			condition = metav1.Condition{
				Type:    slurmv1.ConditionClusterSConfigControllerAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "NotAvailable",
				Message: "Slurm SConfigController is not available yet",
			}
			res.RequeueAfter += requeueDuration
		}
		changesInConditions = status.SetCondition(condition)

		return changesInStatus || changesInConditions
	}); err != nil {
		logger.Error(err, "Failed to update status")
		return res, fmt.Errorf("updating .Status: %w", err)
	}

	return res, nil
}
