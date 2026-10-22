package sharedsteps

import (
	"context"
	"fmt"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func validateWorkersOperational(
	ctx context.Context,
	runtime framework.Runtime,
	slurm *framework.SlurmClient,
	workers []framework.WorkerInfo,
) error {
	if len(workers) == 0 {
		return fmt.Errorf("validate workers are operational: no workers were recorded")
	}

	for _, worker := range workers {
		node, err := slurm.NodeInfo(ctx, worker.Name)
		if err != nil {
			return fmt.Errorf("read worker node state %s: %w", worker.Name, err)
		}
		if node.State == "" {
			return fmt.Errorf("parse worker node state %s: no State field", worker.Name)
		}
		if !node.IsUsable() {
			return fmt.Errorf("worker node %s has bad state=%s reason=%s", worker.Name, node.State, node.Reason)
		}
		if err := framework.RunTargetedWorkerSmoke(ctx, runtime, worker); err != nil {
			return fmt.Errorf("validate worker %s is operational: %w", worker.Name, err)
		}
	}
	return nil
}
