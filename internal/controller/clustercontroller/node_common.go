package clustercontroller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// DeployCommon creates all common resources for Slurm cluster.
func (r SlurmClusterReconciler) DeployCommon(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
	clusterCR *slurmv1.SlurmCluster,
) (ctrl.Result, error) {
	// Deploy Slurm configs ConfigMap
	{
		found := &corev1.ConfigMap{}
		dep, err := common.RenderConfigMapSlurmConfigs(clusterValues)
		if err != nil {
			return ctrl.Result{}, err
		}
		if res, err := r.EnsureDeployed(ctx, &dep, found, clusterCR); err != nil {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}
