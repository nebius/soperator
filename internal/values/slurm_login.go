package values

import (
	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

// SlurmLogin contains the data needed to deploy and reconcile Slurm Login nodes
// TODO login node reconciliation
type SlurmLogin struct{}

func buildSlurmLoginFrom(_ string, _ *slurmv1.SlurmNodeLogin) SlurmLogin {
	return SlurmLogin{}
}
