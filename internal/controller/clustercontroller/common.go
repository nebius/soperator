package clustercontroller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/render/otel"
	"nebius.ai/slurm-operator/internal/render/rest"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// ReconcileCommon reconciles all common resources for Slurm cluster
func (r SlurmClusterReconciler) ReconcileCommon(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) error {
	logger := log.FromContext(ctx)

	reconcileCommonImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of common resources",
			utils.MultiStepExecutionStrategyCollectErrors,
			utils.MultiStepExecutionStep{
				Name: "Slurm JWT secret key for REST API",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := corev1.Secret{}
					if getErr := r.Get(
						stepCtx,
						types.NamespacedName{
							Namespace: clusterValues.Namespace,
							Name:      naming.BuildSecretSlurmRESTSecretName(clusterValues.Name),
						},
						&desired,
					); getErr != nil {
						if !apierrors.IsNotFound(getErr) {
							stepLogger.Error(getErr, "Failed to get")
							return fmt.Errorf("getting REST JWT Key Secret: %w", getErr)
						}

						renderedDesired, err := rest.RenderSecret(clusterValues.Name, clusterValues.Namespace)
						if err != nil {
							stepLogger.Error(err, "Failed to render")
							return fmt.Errorf("rendering REST JWT secret key: %w", err)
						}
						desired = *renderedDesired.DeepCopy()
						stepLogger.V(1).Info("Rendered")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)

					if err := r.Secret.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling REST JWT secret key: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm configs ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					topologyConfig := corev1.ConfigMap{}
					if clusterValues.SlurmTopologyConfigMapRefName != "" {
						stepLogger = stepLogger.WithValues("topologyConfigMapRefName", clusterValues.SlurmTopologyConfigMapRefName)
						if getErr := r.Get(
							stepCtx,
							types.NamespacedName{
								Namespace: clusterValues.Namespace,
								Name:      clusterValues.SlurmTopologyConfigMapRefName,
							},
							&topologyConfig,
						); getErr != nil {
							stepLogger.Error(getErr, "Failed to get topology config")
							return fmt.Errorf("failed to get topology config: %w", getErr)
						}
					}

					desired := common.RenderConfigMapSlurmConfigs(clusterValues, topologyConfig)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling ConfigMap with Slurm configs: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "OpenTelemetry Collector",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					if check.IsOtelCRDInstalled() {
						if check.IsOtelEnabled(clusterValues.Telemetry) {

							var foundPodTemplate *corev1.PodTemplate

							if clusterValues.Telemetry.OpenTelemetryCollector != nil &&
								clusterValues.Telemetry.OpenTelemetryCollector.Enabled &&
								clusterValues.Telemetry.OpenTelemetryCollector.PodTemplateNameRef != nil {

								podTemplateName := *clusterValues.Telemetry.OpenTelemetryCollector.PodTemplateNameRef

								foundPodTemplate = &corev1.PodTemplate{}
								if err := r.Get(
									stepCtx,
									types.NamespacedName{
										Namespace: clusterValues.Namespace,
										Name:      podTemplateName,
									},
									foundPodTemplate,
								); err != nil {
									stepLogger.Error(err, "Failed to get PodTemplate")
									return fmt.Errorf("getting PodTemplate: %w", err)
								}
							}

							desired, err := otel.RenderOtelCollector(
								clusterValues.Name,
								clusterValues.Namespace,
								clusterValues.Telemetry,
								cluster.Spec.SlurmNodes.Exporter.Enabled,
								foundPodTemplate,
							)
							if err != nil {
								stepLogger.Error(err, "Failed to render")
							}

							if desired != nil {
								stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
								stepLogger.V(1).Info("Rendered")
							}

							err = r.Otel.Reconcile(stepCtx, cluster, desired)
							if err != nil {
								stepLogger.Error(err, "Failed to reconcile")
								return fmt.Errorf("reconciling OpenTelemetry Collector: %w", err)
							}

							stepLogger.V(1).Info("Reconciled")
						}
					}
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Munge key Secret",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := corev1.Secret{}
					if getErr := r.Get(
						stepCtx,
						types.NamespacedName{
							Namespace: clusterValues.Namespace,
							Name:      naming.BuildSecretMungeKeyName(clusterValues.Name),
						},
						&desired,
					); getErr != nil {
						if !apierrors.IsNotFound(getErr) {
							stepLogger.Error(getErr, "Failed to get")
							return fmt.Errorf("getting Munge Key Secret: %w", getErr)
						}

						renderedDesired, err := common.RenderMungeKeySecret(clusterValues.Name, clusterValues.Namespace)
						if err != nil {
							stepLogger.Error(err, "Failed to render")
							return fmt.Errorf("rendering Munge Key Secret: %w", err)
						}
						desired = *renderedDesired.DeepCopy()
						stepLogger.V(1).Info("Rendered")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)

					if err := r.Secret.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling Munge Key Secret: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "AppArmor profiles",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")
					if !check.IsAppArmorCRDInstalled() {
						stepLogger.V(1).Info("AppArmor CRD is not installed, skipping AppArmor profile reconciliation")
						return nil
					}
					if !clusterValues.NodeLogin.UseDefaultAppArmorProfile || !clusterValues.NodeWorker.UseDefaultAppArmorProfile {
						stepLogger.V(1).Info("Default AppArmor profile is not set, skipping AppArmor profile reconciliation")
						return nil
					}

					desired := common.RenderAppArmorProfile(
						clusterValues,
					)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.AppArmorProfile.Reconcile(stepCtx, cluster, desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling AppArmor profiles: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")
					return nil
				},
			},
		)
	}

	if err := reconcileCommonImpl(); err != nil {
		logger.Error(err, "Failed to reconcile common resources")
		return fmt.Errorf("reconciling common resources: %w", err)
	}
	logger.Info("Reconciled common resources")
	return nil
}
