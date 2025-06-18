package clustercontroller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/exporter"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// ReconcileSoperatorExporter reconciles all resources for Soperator exporter
func (r SlurmClusterReconciler) ReconcileSoperatorExporter(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) error {
	logger := log.FromContext(ctx)
	if !check.IsPrometheusOperatorCRDInstalled {
		logger.V(1).Info("Prometheus Operator CRD is not installed, skipping reconciliation")
		return nil
	}
	if clusterValues.SlurmExporter.ExporterContainer.Image != "" {
		logger.V(1).Info("Slurm exporter image is set, skipping Soperator exporter reconciliation")
		return nil
	}

	steps := []utils.MultiStepExecutionStep{
		{
			Name: "Exporter SA",
			Func: func(stepCtx context.Context) error {
				debugLogger := log.FromContext(stepCtx).V(1)
				debugLogger.Info("Reconciling")

				if clusterValues.SlurmExporter.Enabled {
					sa := exporter.RenderServiceAccount(clusterValues.Namespace, clusterValues.Name)
					debugLogger.Info("Rendered", logfield.ResourceKV(&sa)...)
					if err := r.ServiceAccount.Reconcile(stepCtx, cluster, &sa); err != nil {
						return fmt.Errorf("reconcile Exporter SA: %w", err)
					}
				} else {
					debugLogger.Info("Exporter disabled, will delete ServiceAccount if exists")
					exporterSAName := exporter.BuildExporterServiceAccountName(clusterValues.Name)
					if err := r.ServiceAccount.Cleanup(stepCtx, cluster, exporterSAName); err != nil {
						return fmt.Errorf("cleanup Exporter SA: %w", err)
					}
				}

				debugLogger.Info("Reconciled")
				return nil
			},
		},
		{
			Name: "Exporter Role",
			Func: func(stepCtx context.Context) error {
				debugLogger := log.FromContext(stepCtx).V(1)
				debugLogger.Info("Reconciling")

				if clusterValues.SlurmExporter.Enabled {
					desired := exporter.RenderRole(clusterValues.Namespace, clusterValues.Name)
					debugLogger.Info("Rendered", logfield.ResourceKV(&desired)...)
					if err := r.Role.Reconcile(stepCtx, cluster, &desired); err != nil {
						return fmt.Errorf("reconcile Exporter Role: %w", err)
					}
				} else {
					debugLogger.Info("Exporter disabled, will delete Role if exists")
					exporterRoleName := exporter.BuildExporterRoleName(clusterValues.Name)
					if err := r.Role.Cleanup(stepCtx, cluster, exporterRoleName); err != nil {
						return fmt.Errorf("cleanup Exporter Role: %w", err)
					}
				}

				debugLogger.Info("Reconciled")
				return nil
			},
		},
		{
			Name: "Exporter RoleBinding",
			Func: func(stepCtx context.Context) error {
				debugLogger := log.FromContext(stepCtx).V(1)
				debugLogger.Info("Reconciling Exporter RoleBinding")

				if clusterValues.SlurmExporter.Enabled {
					rb := exporter.RenderExporterRoleBinding(clusterValues.Namespace, clusterValues.Name)
					debugLogger.Info("Rendered", logfield.ResourceKV(&rb)...)
					if err := r.RoleBinding.Reconcile(stepCtx, cluster, &rb); err != nil {
						return fmt.Errorf("reconcile Exporter RoleBinding: %w", err)
					}
				} else {
					debugLogger.Info("Exporter disabled, will delete RoleBinding if exists")
					exporterRoleBindingName := exporter.BuildExporterRoleBindingName(clusterValues.Name)
					if err := r.RoleBinding.Cleanup(stepCtx, cluster, exporterRoleBindingName); err != nil {
						return fmt.Errorf("cleanup Exporter RoleBinding: %w", err)
					}
				}

				debugLogger.Info("Reconciled")
				return nil
			},
		},
		{
			Name: "PodMonitor",
			Func: func(stepCtx context.Context) error {
				debugLogger := log.FromContext(stepCtx).V(1)
				debugLogger.Info("Reconciling")

				if clusterValues.SlurmExporter.Enabled {
					desired := exporter.RenderPodMonitor(
						clusterValues.Name,
						clusterValues.Namespace,
						clusterValues.SlurmExporter,
					)
					debugLogger.Info("Rendered", logfield.ResourceKV(desired)...)
					if err := r.PodMonitor.Reconcile(stepCtx, cluster, desired); err != nil {
						return fmt.Errorf("reconcile PodMonitor: %w", err)
					}
				} else {
					debugLogger.Info("Exporter disabled, will delete PodMonitor if exists")
					if err := r.PodMonitor.Cleanup(stepCtx, cluster, clusterValues.Name); err != nil {
						return fmt.Errorf("cleanup PodMonitor: %w", err)
					}
				}

				debugLogger.Info("Reconciled")
				return nil
			},
		},
		{
			Name: "Exporter Deployment",
			Func: func(stepCtx context.Context) error {
				debugLogger := log.FromContext(stepCtx).V(1)
				debugLogger.Info("Reconciling")

				if clusterValues.SlurmExporter.Enabled {
					desired, err := exporter.RenderDeploymentExporter(clusterValues)
					if err != nil {
						return fmt.Errorf("render deployment exporter: %w", err)
					}
					debugLogger.Info("Rendered", logfield.ResourceKV(desired)...)
					if err := r.Deployment.Reconcile(stepCtx, cluster, desired, nil); err != nil {
						return fmt.Errorf("reconcile soperator exporter deployment: %w", err)
					}
				} else {
					debugLogger.Info("Exporter disabled, will delete Deployment if exists")
					exporterDeploymentName := naming.BuildDeploymentName(consts.ComponentTypeExporter)
					if err := r.Deployment.Cleanup(stepCtx, cluster, exporterDeploymentName); err != nil {
						return fmt.Errorf("cleanup soperator exporter deployment: %w", err)
					}
				}

				debugLogger.Info("Reconciled")
				return nil
			},
		},
	}

	if err := utils.ExecuteMultiStep(ctx,
		"Reconciliation of soperator exporter",
		utils.MultiStepExecutionStrategyCollectErrors,
		steps...,
	); err != nil {
		return fmt.Errorf("reconcile soperator exporter: %w", err)
	}
	logger.Info("Reconciled soperator exporter")
	return nil
}
