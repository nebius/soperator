package clustercontroller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/logfield"
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
					debugLogger.Info("Rendered", logfield.ResourceKV(&desired)...)
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
					if err := r.Deployment.Reconcile(stepCtx, cluster, *desired); err != nil {
						return fmt.Errorf("reconcile soperator exporter deployment: %w", err)
					}
				} else {
					debugLogger.Info("Exporter disabled, will delete Deployment if exists")
					exporterDeploymentName := exporter.DeploymentName
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
