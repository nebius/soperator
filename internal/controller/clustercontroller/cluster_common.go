package clustercontroller

import (
	"context"

	"github.com/pkg/errors"
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
	slurmprometheus "nebius.ai/slurm-operator/internal/render/prometheus"
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
				Name: "Slurm configs ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired, err := common.RenderConfigMapSlurmConfigs(clusterValues)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering ConfigMap with Slurm configs")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.Info("Rendered")

					if err = r.ConfigMap.Reconcile(ctx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling ConfigMap with Slurm configs")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "OpenTelemetry Collector",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					if check.IsOtelCRDInstalled() {
						if check.IsOtelEnabled(clusterValues.Telemetry) {

							var foundPodTemplate *corev1.PodTemplate = nil

							if clusterValues.Telemetry.OpenTelemetryCollector != nil &&
								clusterValues.Telemetry.OpenTelemetryCollector.Enabled &&
								clusterValues.Telemetry.OpenTelemetryCollector.PodTemplateNameRef != nil {

								podTemplateName := *clusterValues.Telemetry.OpenTelemetryCollector.PodTemplateNameRef

								if err := r.Get(
									ctx,
									types.NamespacedName{
										Namespace: clusterValues.Namespace,
										Name:      podTemplateName,
									},
									foundPodTemplate,
								); err != nil {
									stepLogger.Error(err, "Failed to get PodTemplate")
									return errors.Wrap(err, "getting PodTemplate")
								}
							}

							desired, err := otel.RenderOtelCollector(
								clusterValues.Name, clusterValues.Namespace, clusterValues.Telemetry, foundPodTemplate,
							)
							if err != nil {
								stepLogger.Error(err, "Failed to render")
							}

							if desired != nil {
								stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
								stepLogger.Info("Rendered")
							}

							err = r.Otel.Reconcile(ctx, cluster, desired)
							if err != nil {
								stepLogger.Error(err, "Failed to reconcile")
								return errors.Wrap(err, "reconciling OpenTelemetry Collector")
							}

							stepLogger.Info("Reconciled")
						}
					}
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Munge key Secret",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					desired := corev1.Secret{}
					if getErr := r.Get(
						ctx,
						types.NamespacedName{
							Namespace: clusterValues.Namespace,
							Name:      naming.BuildSecretMungeKeyName(clusterValues.Name),
						},
						&desired,
					); getErr != nil {
						if !apierrors.IsNotFound(getErr) {
							stepLogger.Error(getErr, "Failed to get")
							return errors.Wrap(getErr, "getting Munge Key Secret")
						}

						renderedDesired, err := common.RenderMungeKeySecret(clusterValues.Name, clusterValues.Namespace)
						if err != nil {
							stepLogger.Error(err, "Failed to render")
							return errors.Wrap(err, "rendering Munge Key Secret")
						}
						desired = *renderedDesired.DeepCopy()
						stepLogger.Info("Rendered")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)

					if err := r.Secret.Reconcile(ctx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling Munge Key Secret")
					}
					stepLogger.Info("Reconciled")

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "PodMonitor",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")

					if check.IsPrometheusOperatorCRDInstalled {
						if check.IsPrometheusEnabled(clusterValues.Telemetry) {
							desired, err := slurmprometheus.RenderPodMonitor(
								clusterValues.Name, clusterValues.Namespace, clusterValues.Telemetry,
							)
							if err != nil {
								stepLogger.Error(err, "Failed to render")
							}
							if desired != nil {
								stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
							}
							err = r.PodMonitor.Reconcile(ctx, cluster, desired)
							if err != nil {
								stepLogger.Error(err, "Failed to reconcile")
								return errors.Wrap(err, "reconciling PodMonitor")
							}
							stepLogger.Info("Reconciled")
						}
					}

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Exporter",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.Info("Reconciling")
					if check.IsPrometheusOperatorCRDInstalled {
						if check.IsPrometheusEnabled(clusterValues.Telemetry) {
							var foundPodTemplate *corev1.PodTemplate = nil

							if clusterValues.Telemetry.Prometheus.PodTemplateNameRef != nil {
								podTemplateName := *clusterValues.Telemetry.Prometheus.PodTemplateNameRef

								err := r.Get(
									ctx,
									types.NamespacedName{
										Namespace: clusterValues.Namespace,
										Name:      podTemplateName,
									},
									foundPodTemplate,
								)
								if err != nil {
									stepLogger.Error(err, "Failed to get PodTemplate")
									return errors.Wrap(err, "getting PodTemplate")
								}
							}
							desired, err := slurmprometheus.RenderDeploymentExporter(
								clusterValues.Name,
								clusterValues.Namespace,
								&clusterValues.SlurmExporter,
								clusterValues.NodeFilters,
								clusterValues.VolumeSources,
								foundPodTemplate,
							)
							if err != nil {
								stepLogger.Error(err, "Failed to render")
							}
							if desired != nil {
								logger = logger.WithValues(logfield.ResourceKV(desired)...)
							}
							err = r.SlurmExporter.Reconcile(ctx, cluster, desired)
							if err != nil {
								stepLogger.Error(err, "Failed to reconcile")
								return errors.Wrap(err, "reconciling Slurm Exporter Deployment")
							}
							stepLogger.Info("Reconciled")
						}
					}
					return nil
				},
			},
		)
	}

	if err := reconcileCommonImpl(); err != nil {
		logger.Error(err, "Failed to reconcile common resources")
		return errors.Wrap(err, "reconciling common resources")
	}
	logger.Info("Reconciled common resources")
	return nil
}
