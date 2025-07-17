package values

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

type SlurmCluster struct {
	types.NamespacedName

	CRVersion              string
	ClusterType            consts.ClusterType
	PartitionConfiguration PartitionConfiguration
	WorkerFeatures         []slurmv1.WorkerFeature
	HealthCheckConfig      *HealthCheckConfig

	PopulateJail PopulateJail

	NCCLBenchmark SlurmNCCLBenchmark

	NodeFilters   []slurmv1.K8sNodeFilter
	VolumeSources []slurmv1.VolumeSource
	Secrets       slurmv1.Secrets

	NodeController                SlurmController
	NodeAccounting                SlurmAccounting
	NodeRest                      SlurmREST
	NodeWorker                    SlurmWorker
	NodeLogin                     SlurmLogin
	SlurmExporter                 SlurmExporter
	SlurmConfig                   slurmv1.SlurmConfig
	CustomSlurmConfig             *string
	MPIConfig                     slurmv1.MPIConfig
	PlugStackConfig               slurmv1.PlugStackConfig
	SlurmTopologyConfigMapRefName string
	SConfigController             SConfigController
}

// BuildSlurmClusterFrom creates a new instance of SlurmCluster given a SlurmCluster CRD
func BuildSlurmClusterFrom(ctx context.Context, cluster *slurmv1.SlurmCluster) (*SlurmCluster, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info(fmt.Sprintf("%+v", cluster.Spec.SConfigController))

	clusterType, err := consts.StringToClusterType(cluster.Spec.ClusterType)
	if err != nil {
		logger.Error(err, "Failed to get cluster type")
		return nil, fmt.Errorf("getting cluster type: %w", err)
	}

	res := &SlurmCluster{
		NamespacedName: types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		CRVersion:              buildCRVersionFrom(ctx, cluster.Spec.CRVersion),
		ClusterType:            clusterType,
		PartitionConfiguration: buildPartitionConfiguration(&cluster.Spec.PartitionConfiguration),
		WorkerFeatures:         cluster.Spec.WorkerFeatures,
		HealthCheckConfig:      buildHealthCheckConfig(cluster.Spec.HealthCheckConfig),
		NCCLBenchmark:          buildSlurmNCCLBenchmarkFrom(cluster.Name, &cluster.Spec.PeriodicChecks.NCCLBenchmark),
		PopulateJail:           buildSlurmPopulateJailFrom(cluster.Name, cluster.Spec.Maintenance, &cluster.Spec.PopulateJail),
		NodeFilters:            buildNodeFiltersFrom(cluster.Spec.K8sNodeFilters),
		VolumeSources:          buildVolumeSourcesFrom(cluster.Spec.VolumeSources),
		Secrets:                buildSecretsFrom(&cluster.Spec.Secrets),
		NodeController:         buildSlurmControllerFrom(cluster.Name, cluster.Spec.Maintenance, &cluster.Spec.SlurmNodes.Controller),
		NodeAccounting:         buildAccountingFrom(cluster.Name, cluster.Spec.Maintenance, &cluster.Spec.SlurmNodes.Accounting),
		NodeRest:               buildRestFrom(cluster.Name, cluster.Spec.Maintenance, &cluster.Spec.SlurmNodes.Rest),
		NodeWorker: buildSlurmWorkerFrom(
			cluster.Name,
			cluster.Spec.Maintenance,
			&cluster.Spec.SlurmNodes.Worker,
			&cluster.Spec.NCCLSettings,
			cluster.Spec.UseDefaultAppArmorProfile,
		),
		NodeLogin:         buildSlurmLoginFrom(cluster.Name, cluster.Spec.Maintenance, &cluster.Spec.SlurmNodes.Login, cluster.Spec.UseDefaultAppArmorProfile),
		SlurmExporter:     buildSlurmExporterFrom(cluster.Spec.Maintenance, &cluster.Spec.SlurmNodes.Exporter),
		SlurmConfig:       cluster.Spec.SlurmConfig,
		CustomSlurmConfig: cluster.Spec.CustomSlurmConfig,
		MPIConfig:         cluster.Spec.MPIConfig,
		PlugStackConfig:   cluster.Spec.PlugStackConfig,
		SConfigController: buildSConfigControllerFrom(
			cluster.Spec.SConfigController.Node,
			cluster.Spec.SConfigController.Container,
			*cluster.Spec.Maintenance,
			cluster.Spec.SConfigController.JailSlurmConfigPath,
		),
	}

	if err := res.Validate(ctx); err != nil {
		logger.Error(err, "SlurmCluster validation failed")
		return res, fmt.Errorf("failed to validate SlurmCluster: %w", err)
	}

	return res, nil
}
