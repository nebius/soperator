package values

import (
	"context"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

// SlurmWorker contains the data needed to deploy and reconcile the Slurm Workers
// TODO workers reconciliation
type SlurmWorker struct{}

func buildSlurmWorkerFrom(_ context.Context, _ *slurmv1.SlurmCluster) (SlurmWorker, error) {
	return SlurmWorker{}, nil
}
