package clustercontroller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/render/exporter"
	"nebius.ai/slurm-operator/internal/render/rest"
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
		// TODO: do we really need to check this?
		logger.V(1).Info("Prometheus Operator CRD is not installed, skipping reconciliation")
		return nil
	}
	if !clusterValues.SlurmExporter.Enabled {
		logger.V(1).Info("Soperator exporter is not enabled, skipping reconciliation")
		return nil
	}
	if clusterValues.SlurmExporter.ExporterContainer.Image != "" {
		logger.V(1).Info("Slurm exporter image is set, skipping Soperator exporter reconciliation")
	}

	steps := []utils.MultiStepExecutionStep{
		{
			Name: "Exporter SA",
			Func: func(stepCtx context.Context) error {
				debugLogger := log.FromContext(stepCtx).V(1)
				debugLogger.Info("Reconciling")
				desired := exporter.RenderServiceAccount(clusterValues.Namespace, clusterValues.Name)
				debugLogger.Info("Rendered", logfield.ResourceKV(&desired)...)
				if err := r.ServiceAccount.Reconcile(stepCtx, cluster, &desired); err != nil {
					return fmt.Errorf("reconcile Exporter SA: %w", err)
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
				desired := exporter.RenderRole(clusterValues.Namespace, clusterValues.Name)
				debugLogger.Info("Rendered", logfield.ResourceKV(&desired)...)
				if err := r.Role.Reconcile(stepCtx, cluster, &desired); err != nil {
					return fmt.Errorf("reconcile Exporter Role: %w", err)
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
				desired := exporter.RenderExporterRoleBinding(clusterValues.Namespace, clusterValues.Name)
				debugLogger.Info("Rendered", logfield.ResourceKV(&desired)...)
				if err := r.RoleBinding.Reconcile(stepCtx, cluster, &desired); err != nil {
					return fmt.Errorf("reconcile Exporter RoleBinding: %w", err)
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
				desired, err := exporter.RenderPodMonitor(
					clusterValues.Name,
					clusterValues.Namespace,
					clusterValues.SlurmExporter,
				)
				if err != nil {
					return fmt.Errorf("render pod monitor: %w", err)
				}
				debugLogger.Info("Rendered", logfield.ResourceKV(desired)...)
				if err = r.PodMonitor.Reconcile(stepCtx, cluster, desired); err != nil {
					return fmt.Errorf("reconcile PodMonitor: %w", err)
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
				var podTemplatePatch *corev1.PodTemplate
				if clusterValues.SlurmExporter.PodTemplatePatchNameRef != nil {
					podTemplatePatch = &corev1.PodTemplate{}
					err := r.Get(
						stepCtx,
						types.NamespacedName{
							Namespace: clusterValues.Namespace,
							Name:      *clusterValues.SlurmExporter.PodTemplatePatchNameRef,
						},
						podTemplatePatch,
					)
					if err != nil {
						return fmt.Errorf("get PodTemplate: %w", err)
					}
				}
				desired, err := exporter.RenderDeploymentExporter(
					clusterValues.Name,
					clusterValues.Namespace,
					clusterValues.SlurmExporter,
					clusterValues.NodeFilters,
					podTemplatePatch,
					rest.GetServiceURL(clusterValues.Namespace, &clusterValues.NodeRest),
				)
				if err != nil {
					return fmt.Errorf("render deployment exporter: %w", err)
				}
				debugLogger.Info("Rendered", logfield.ResourceKV(desired)...)
				var exporterNamePtr *string
				if err := r.Deployment.Reconcile(stepCtx, cluster, desired, exporterNamePtr); err != nil {
					return fmt.Errorf("reconcile soperator exporter deployment: %w", err)
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
