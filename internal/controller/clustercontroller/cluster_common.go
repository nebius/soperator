package clustercontroller

import (
	"context"

	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/render/common"
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
		return nil
	}

	if err := reconcileCommonImpl(); err != nil {
		logger.Error(err, "Failed to reconcile common resources")
		return errors.Wrap(err, "reconciling common resources")
	}
	return nil
}
