package clustercontroller

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/logfield"
	slurmprometheus "nebius.ai/slurm-operator/internal/render/prometheus"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// ReconcileExporter reconciles all resources for Slurm exporter
func (r SlurmClusterReconciler) ReconcileExporter(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) error {
	logger := log.FromContext(ctx)

	reconcileExporterImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of exporter resources",
			utils.MultiStepExecutionStrategyCollectErrors,
			utils.MultiStepExecutionStep{
				Name: "PodMonitor",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					if check.IsPrometheusOperatorCRDInstalled {
						stepLogger.V(1).Info("Prometheus Operator CRD is installed")
						if check.IsPrometheusEnabled(&clusterValues.SlurmExporter) {
							stepLogger.V(1).Info("Prometheus is enabled")
							desired, err := slurmprometheus.RenderPodMonitor(
								clusterValues.Name,
								clusterValues.Namespace,
								&clusterValues.SlurmExporter,
							)
							if err != nil {
								stepLogger.Error(err, "Failed to render")
							}
							if desired != nil {
								stepLogger = stepLogger.WithValues(logfield.ResourceKV(desired)...)
							}
							err = r.PodMonitor.Reconcile(stepCtx, cluster, desired)
							if err != nil {
								stepLogger.Error(err, "Failed to reconcile")
								return errors.Wrap(err, "reconciling PodMonitor")
							}
							stepLogger.V(1).Info("Reconciled")
						}
					}

					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Exporter",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")
					if check.IsPrometheusOperatorCRDInstalled {
						stepLogger.V(1).Info("Prometheus Operator CRD is installed")
						if check.IsPrometheusEnabled(&clusterValues.SlurmExporter) {
							stepLogger.V(1).Info("Prometheus is enabled")
							var foundPodTemplate *corev1.PodTemplate

							if clusterValues.SlurmExporter.PodTemplateNameRef != nil {
								podTemplateName := *clusterValues.SlurmExporter.PodTemplateNameRef

								foundPodTemplate = &corev1.PodTemplate{}
								err := r.Get(
									stepCtx,
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
							var exporterNamePtr *string = nil
							err = r.Deployment.Reconcile(stepCtx, cluster, desired, exporterNamePtr)
							if err != nil {
								stepLogger.Error(err, "Failed to reconcile")
								return errors.Wrap(err, "reconciling Slurm Exporter Deployment")
							}
							stepLogger.V(1).Info("Reconciled")
						}
					}
					return nil
				},
			},
		)
	}

	if err := reconcileExporterImpl(); err != nil {
		logger.Error(err, "Failed to reconcile exporter resources")
		return errors.Wrap(err, "reconciling exporter resources")
	}
	logger.Info("Reconciled exporter resources")
	return nil
}
