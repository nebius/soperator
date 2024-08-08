package clustercontroller

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/render/otel"
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
		// Slurm configs
		{
			desired, err := common.RenderConfigMapSlurmConfigs(clusterValues)
			if err != nil {
				logger.Error(err, "Failed to render ConfigMap with Slurm configs")
				return errors.Wrap(err, "rendering ConfigMap with Slurm configs")
			}
			logger = logger.WithValues(logfield.ResourceKV(&desired)...)

			err = r.ConfigMap.Reconcile(ctx, cluster, &desired)
			if err != nil {
				logger.Error(err, "Failed to reconcile ConfigMap with Slurm configs")
				return errors.Wrap(err, "reconciling ConfigMap with Slurm configs")
			}
			logger.Info("Reconcile for SlurmConfigs configMap completed successfully")
		}

		// OpenTelemetry Collector
		{
			foundPodTemplate := &corev1.PodTemplate{}
			if clusterValues.Metrics != nil && clusterValues.Metrics.EnableOtelCollector != nil {

				if clusterValues.Metrics.PodTemplateNameRef != nil {
					podTemplateName := *clusterValues.Metrics.PodTemplateNameRef

					err := r.Get(
						ctx,
						types.NamespacedName{
							Namespace: clusterValues.Namespace,
							Name:      podTemplateName,
						},
						foundPodTemplate,
					)
					if err != nil {
						logger.Error(err, "Failed to get PodTemplate")
						return errors.Wrap(err, "getting PodTemplate")
					}
				}
			}
			desired, err := otel.RenderOtelCollector(clusterValues.Name, clusterValues.Namespace, clusterValues.Metrics, foundPodTemplate)
			if err != nil {
				err = r.Otel.Reconcile(ctx, cluster, &desired, false)
			} else {
				err = r.Otel.Reconcile(ctx, cluster, &desired, true)

			}
			logger = logger.WithValues(logfield.ResourceKV(&desired)...)
			if err != nil {
				logger.Error(err, "Failed to reconcile OpenTelemetry Collector")
				return errors.Wrap(err, "reconciling OpenTelemetry Collector")
			}
			logger.Info("Reconcile for OpenTelemetry Collector completed successfully")
		}

		// Munge secret
		{
			desired := corev1.Secret{}
			if err := r.Get(
				ctx,
				types.NamespacedName{
					Namespace: clusterValues.Namespace,
					Name:      naming.BuildSecretMungeKeyName(clusterValues.Name),
				},
				&desired,
			); err != nil {
				if apierrors.IsNotFound(err) {
					renderedDesired, err := common.RenderMungeKeySecret(clusterValues.Name, clusterValues.Namespace)
					desired = *renderedDesired.DeepCopy()
					if err != nil {
						logger.Error(err, "Failed to render Munge Key Secret")
						return errors.Wrap(err, "rendering Munge Key Secret")
					}
					logger = logger.WithValues(logfield.ResourceKV(&desired)...)
					err = r.Secret.Reconcile(ctx, cluster, &desired)
					if err != nil {
						logger.Error(err, "Failed to reconcile Munge Key Secret")
						return errors.Wrap(err, "reconciling Munge Key Secret")
					}
				} else {
					logger.Error(err, "Failed to get Munge Key Secret")
					return errors.Wrap(err, "getting Munge Key Secret")
				}
			}
			logger.Info("Reconcile for munge key secret completed successfully")
		}

		return nil
	}

	if err := reconcileCommonImpl(); err != nil {
		logger.Error(err, "Failed to reconcile common resources")
		return errors.Wrap(err, "reconciling common resources")
	}
	return nil
}
