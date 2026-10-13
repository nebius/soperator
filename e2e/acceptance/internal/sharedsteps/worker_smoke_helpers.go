package sharedsteps

import (
	"context"
	"fmt"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const targetedWorkerSmokeTimeout = 3 * time.Minute

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
