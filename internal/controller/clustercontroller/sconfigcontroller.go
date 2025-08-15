package clustercontroller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
				Name: "SCoonfigController ServiceAccount",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling SCoonfigController ServiceAccount")

					desired := sconfigcontroller.RenderServiceAccount(clusterValues.Namespace, clusterValues.Name)

					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.ServiceAccount.Reconcile(stepCtx, cluster, desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling SConfigController ServiceAccount: %w", err)
					}

					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "SCoonfigController Role",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling SCoonfigController Role")

					desired := sconfigcontroller.RenderRole(clusterValues.Namespace, clusterValues.Name)

					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.Role.Reconcile(stepCtx, cluster, desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling SConfigController Role: %w", err)
					}

					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "SCoonfigController RoleBinding",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling SCoonfigController RoleBinding")

					desired := sconfigcontroller.RenderRoleBinding(clusterValues.Namespace, clusterValues.Name)

					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.RoleBinding.Reconcile(stepCtx, cluster, desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling SConfigController RoleBinding: %w", err)
					}

					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},
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
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
		}
		logger.Error(err, "Failed to get SConfigController Deployment")
		return ctrl.Result{}, fmt.Errorf("getting SConfigController Deployment: %w", err)
	}

	targetReplicas := int32(1)
	if existing.Spec.Replicas != nil {
		targetReplicas = *existing.Spec.Replicas
	}
	if existing.Status.AvailableReplicas != targetReplicas {
		if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
			status.SetCondition(metav1.Condition{
				Type:   slurmv1.ConditionClusterSConfigControllerAvailable,
				Status: metav1.ConditionFalse, Reason: "NotAvailable",
				Message: "Slurm SConfigController is not available yet",
			})
			status.ReadySConfigController = &existing.Status.AvailableReplicas
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	} else {
		if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
			status.SetCondition(metav1.Condition{
				Type:   slurmv1.ConditionClusterSConfigControllerAvailable,
				Status: metav1.ConditionTrue, Reason: "Available",
				Message: "Slurm SConfigController is available",
			})
			status.ReadySConfigController = &existing.Status.AvailableReplicas
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}
