package values

import slurmv1 "nebius.ai/slurm-operator/api/v1"

type PartitionConfiguration struct {
	ConfigType string
	RawConfig  string
}

func buildPartitionConfiguration(partitionConfiguration *slurmv1.PartitionConfiguration) PartitionConfiguration {
	return PartitionConfiguration{
		ConfigType: partitionConfiguration.ConfigType,
		RawConfig:  partitionConfiguration.RawConfig,
	}
}
