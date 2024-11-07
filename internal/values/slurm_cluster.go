package values

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

type SlurmCluster struct {
	types.NamespacedName

	CRVersion              string
	Pause                  bool
	ClusterType            consts.ClusterType
	PartitionConfiguration PartitionConfiguration

	PopulateJail PopulateJail

	NCCLBenchmark SlurmNCCLBenchmark

	NodeFilters   []slurmv1.K8sNodeFilter
	VolumeSources []slurmv1.VolumeSource
	Secrets       slurmv1.Secrets

	NodeController SlurmController
	NodeAccounting SlurmAccounting
	NodeRest       SlurmREST
	NodeWorker     SlurmWorker
	NodeLogin      SlurmLogin
	Telemetry      *slurmv1.Telemetry
	SlurmExporter  SlurmExporter
}

// BuildSlurmClusterFrom creates a new instance of SlurmCluster given a SlurmCluster CRD
func BuildSlurmClusterFrom(ctx context.Context, cluster *slurmv1.SlurmCluster) (*SlurmCluster, error) {
	logger := log.FromContext(ctx)

	clusterType, err := consts.StringToClusterType(cluster.Spec.ClusterType)
	if err != nil {
		logger.Error(err, "Failed to get cluster type")
		return nil, errors.Wrap(err, "getting cluster type")
	}

	res := &SlurmCluster{
		NamespacedName: types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		CRVersion:              buildCRVersionFrom(ctx, cluster.Spec.CRVersion),
		Pause:                  cluster.Spec.Pause,
		ClusterType:            clusterType,
		PartitionConfiguration: buildPartitionConfiguration(&cluster.Spec.PartitionConfiguration),
		NCCLBenchmark:          buildSlurmNCCLBenchmarkFrom(cluster.Name, &cluster.Spec.PeriodicChecks.NCCLBenchmark),
		PopulateJail:           buildSlurmPopulateJailFrom(cluster.Name, &cluster.Spec.PopulateJail),
		NodeFilters:            buildNodeFiltersFrom(cluster.Spec.K8sNodeFilters),
		VolumeSources:          buildVolumeSourcesFrom(cluster.Spec.VolumeSources),
		Secrets:                buildSecretsFrom(&cluster.Spec.Secrets),
		NodeController:         buildSlurmControllerFrom(cluster.Name, &cluster.Spec.SlurmNodes.Controller),
		NodeAccounting:         buildAccountingFrom(cluster.Name, &cluster.Spec.SlurmNodes.Accounting),
		NodeRest:               buildRestFrom(cluster.Name, &cluster.Spec.SlurmNodes.Rest),
		NodeWorker:             buildSlurmWorkerFrom(cluster.Name, &cluster.Spec.SlurmNodes.Worker, &cluster.Spec.NCCLSettings),
		NodeLogin:              buildSlurmLoginFrom(cluster.Name, &cluster.Spec.SlurmNodes.Login),
		Telemetry:              cluster.Spec.Telemetry,
		SlurmExporter:          buildSlurmExporterFrom(&cluster.Spec.SlurmNodes.Exporter),
	}

	if err := res.Validate(ctx); err != nil {
		logger.Error(err, "SlurmCluster validation failed")
		return res, fmt.Errorf("failed to validate SlurmCluster: %w", err)
	}

	return res, nil
}
