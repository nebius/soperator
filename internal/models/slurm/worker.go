package smodels

import (
	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

// WorkerValues contains the data needed to deploy and reconcile the Slurm Workers
// TODO workers reconciliation
type WorkerValues struct{}

func BuildWorkerValuesFrom(_ *slurmv1.SlurmCluster) (*WorkerValues, error) {
	return &WorkerValues{}, nil
}
