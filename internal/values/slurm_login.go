package values

import (
	"context"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

// SlurmLogin contains the data needed to deploy and reconcile Slurm Login nodes
// TODO login node reconciliation
type SlurmLogin struct{}

func buildSlurmLoginFrom(_ context.Context, _ *slurmv1.SlurmCluster) (SlurmLogin, error) {
	return SlurmLogin{}, nil
}
