package values

import (
	"fmt"
	"strings"

	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	topologyrefs "nebius.ai/slurm-operator/internal/utils/slurm/topology"
)

// buildSlurmConfigFrom copies the SlurmConfig spec and fills in defaults that Slurm must not be
// left to pick on its own. Rendering walks these fields by reflection and skips nil pointers, so
// an unset field would drop the property from slurm.conf entirely.
func buildSlurmConfigFrom(slurmConfig *slurmv1.SlurmConfig) slurmv1.SlurmConfig {
	res := *slurmConfig

	if res.ResumeTimeout == nil {
		// Slurm's own default is 60 seconds, which no worker pod can meet. Every ephemeral
		// resume would then hit ResumeFailProgram and tear the pod back down.
		res.ResumeTimeout = ptr.To[int32](consts.SlurmDefaultResumeTimeout)
	}

	return res
}

type PartitionConfiguration struct {
	ConfigType string
	RawConfig  []string
	Partitions []slurmv1.Partition
}

func buildPartitionConfiguration(partitionConfiguration *slurmv1.PartitionConfiguration) PartitionConfiguration {
	return PartitionConfiguration{
		ConfigType: partitionConfiguration.ConfigType,
		RawConfig:  partitionConfiguration.RawConfig,
		Partitions: partitionConfiguration.Partitions,
	}
}

type HealthCheckConfig struct {
	HealthCheckInterval  int32
	HealthCheckProgram   string
	HealthCheckNodeState []slurmv1.HealthCheckNodeState
}

func buildHealthCheckConfig(healthCheckConfig *slurmv1.HealthCheckConfig) *HealthCheckConfig {
	if healthCheckConfig == nil {
		return nil
	}

	return &HealthCheckConfig{
		HealthCheckInterval:  healthCheckConfig.HealthCheckInterval,
		HealthCheckProgram:   healthCheckConfig.HealthCheckProgram,
		HealthCheckNodeState: healthCheckConfig.HealthCheckNodeState,
	}
}

// BuildNodeSetTopologyBindings encodes the named topologies covering a NodeSet as a comma-separated
// "name=kind" list for the worker init container. Slurm lets a node belong to several topologies at
// once, so every topology referencing the NodeSet is listed.
//
// A CPU-only NodeSet belongs to the generated flat topology and to nothing else, mirroring what the
// topology controller writes into topology.yaml.
func BuildNodeSetTopologyBindings(topology *slurmv1.Topology, nodeSet *slurmv1alpha1.NodeSet) string {
	if topology == nil || len(topology.Topologies) == 0 {
		return ""
	}

	if !nodeSet.Spec.GPU.Enabled {
		return fmt.Sprintf("%s=%s", consts.SlurmTopologyCPUOnlyName, consts.SlurmTopologyTypeFlat)
	}

	var bindings []string
	for _, named := range topology.Topologies {
		if !topologyrefs.CoversNodeSet(named.NodeSetRefs, nodeSet.Name) {
			continue
		}
		bindings = append(bindings, fmt.Sprintf("%s=%s", named.Name, named.Topo.Type))
	}

	return strings.Join(bindings, ",")
}
