package clustercontroller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
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
