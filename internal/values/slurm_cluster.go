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

	NCCLBenchmark SlurmNCCLBenchmark

	NodeFilters   []slurmv1.K8sNodeFilter
	VolumeSources []slurmv1.VolumeSource
	Secrets       slurmv1.Secrets

	NodeController SlurmController
	NodeWorker     SlurmWorker
	NodeLogin      SlurmLogin
}

// BuildSlurmClusterFrom creates a new instance of SlurmCluster given a SlurmCluster CRD
func BuildSlurmClusterFrom(ctx context.Context, cluster *slurmv1.SlurmCluster) (*SlurmCluster, error) {
	res := &SlurmCluster{
		NamespacedName: types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		CRVersion:      buildCRVersionFrom(ctx, cluster.Spec.CRVersion),
		Pause:          cluster.Spec.Pause,
		NCCLBenchmark:  buildSlurmNCCLBenchmarkFrom(cluster.Name, &cluster.Spec.PeriodicChecks.NCCLBenchmark),
		NodeFilters:    buildNodeFiltersFrom(cluster.Spec.K8sNodeFilters),
		VolumeSources:  buildVolumeSourcesFrom(cluster.Spec.VolumeSources),
		Secrets:        buildSecretsFrom(&cluster.Spec.Secrets),
		NodeController: buildSlurmControllerFrom(cluster.Name, &cluster.Spec.SlurmNodes.Controller),
		NodeWorker:     buildSlurmWorkerFrom(cluster.Name, &cluster.Spec.SlurmNodes.Worker),
		NodeLogin:      buildSlurmLoginFrom(cluster.Name, &cluster.Spec.SlurmNodes.Login),
	}

	if err := res.Validate(ctx); err != nil {
		log.FromContext(ctx).Error(err, "SlurmCluster validation failed")
		return res, fmt.Errorf("failed to validate SlurmCluster: %w", err)
	}

	return res, nil
}
