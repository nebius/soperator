package values

import (
	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

// SlurmWorker contains the data needed to deploy and reconcile the Slurm Workers
// TODO workers reconciliation
type SlurmWorker struct{}

func buildSlurmWorkerFrom(_ *slurmv1.SlurmCluster) SlurmWorker {
	return SlurmWorker{}
}
