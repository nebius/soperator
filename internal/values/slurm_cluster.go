package values

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

type SlurmCluster struct {
	types.NamespacedName

	CRVersion string
	Pause     bool

	NodeFilters   []slurmv1.K8sNodeFilter
	VolumeSources []slurmv1.VolumeSource
	Secrets       slurmv1.Secrets

	ConfigMapSlurmConfigs K8sConfigMap

	NodeController SlurmController
	NodeWorker     SlurmWorker
	NodeLogin      SlurmLogin
	NodeDatabase   SlurmDatabase
}

// BuildSlurmClusterFrom creates a new instance of SlurmCluster given a SlurmCluster CRD
func BuildSlurmClusterFrom(ctx context.Context, cluster *slurmv1.SlurmCluster) (*SlurmCluster, error) {
	res := &SlurmCluster{
		NamespacedName: types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		CRVersion:             buildCRVersionFrom(ctx, cluster),
		Pause:                 cluster.Spec.Pause,
		NodeFilters:           buildNodeFiltersFrom(cluster),
		VolumeSources:         buildVolumeSourcesFrom(cluster),
		Secrets:               buildSecretsFrom(cluster),
		ConfigMapSlurmConfigs: buildSlurmConfigFrom(cluster),
		NodeController:        buildSlurmControllerFrom(cluster),
		NodeWorker:            buildSlurmWorkerFrom(cluster),
		NodeLogin:             buildSlurmLoginFrom(cluster),
		NodeDatabase:          buildSlurmDatabaseFrom(cluster),
	}

	if err := res.Validate(ctx); err != nil {
		log.FromContext(ctx).Error(err, "SlurmCluster validation failed")
		return res, fmt.Errorf("failed to validate SlurmCluster: %w", err)
	}

	return res, nil
}
