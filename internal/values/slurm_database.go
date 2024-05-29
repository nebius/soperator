package values

import (
	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

// SlurmDatabase contains the data needed for configuration of database
// TODO database configuration
type SlurmDatabase struct{}

func buildSlurmDatabaseFrom(_ *slurmv1.SlurmCluster) SlurmDatabase {
	return SlurmDatabase{}
}
