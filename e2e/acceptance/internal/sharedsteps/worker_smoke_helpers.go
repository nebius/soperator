package sharedsteps

import (
	"context"
	"fmt"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const targetedWorkerSmokeTimeout = 3 * time.Minute

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
		if err := runTargetedWorkerSmoke(ctx, runtime, worker); err != nil {
			return fmt.Errorf("validate worker %s is operational: %w", worker.Name, err)
		}
	}
	return nil
}

func runTargetedWorkerSmoke(
	ctx context.Context,
	runtime framework.Runtime,
	worker framework.WorkerInfo,
) error {
	command, err := targetedWorkerSmokeCommand(worker)
	if err != nil {
		return err
	}
	jobCtx, cancel := context.WithTimeout(ctx, targetedWorkerSmokeTimeout+time.Minute)
	defer cancel()
	if _, err := runtime.Jail().Run(jobCtx, command); err != nil {
		return fmt.Errorf("run targeted smoke job on worker %s: %w", worker.Name, err)
	}
	return nil
}

func targetedWorkerSmokeCommand(worker framework.WorkerInfo) (string, error) {
	if worker.Name == "" {
		return "", fmt.Errorf("build targeted worker smoke command: worker name is empty")
	}

	if worker.HasGPU {
		return fmt.Sprintf("timeout %.0f srun -w %s --gpus-per-node=1 nvidia-smi -L >/dev/null",
			targetedWorkerSmokeTimeout.Seconds(), framework.ShellQuote(worker.Name)), nil
	}
	return fmt.Sprintf("timeout %.0f srun -w %s hostname",
		targetedWorkerSmokeTimeout.Seconds(), framework.ShellQuote(worker.Name)), nil
}
