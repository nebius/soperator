package framework

import (
	"context"
	"fmt"
	"time"
)

const targetedWorkerSmokeCommandTimeout = 3 * time.Minute

// TargetedWorkerSmokeTimeout includes the remote command's three-minute limit
// and one additional minute for command execution overhead.
const TargetedWorkerSmokeTimeout = targetedWorkerSmokeCommandTimeout + time.Minute

// RunTargetedWorkerSmoke runs hostname on a CPU worker or nvidia-smi -L with one
// GPU allocated on a GPU worker. The worker's HasGPU field selects the command.
// The check is bounded by TargetedWorkerSmokeTimeout and the caller's context.
func RunTargetedWorkerSmoke(ctx context.Context, runtime Runtime, worker WorkerInfo) error {
	command, err := targetedWorkerSmokeCommand(worker)
	if err != nil {
		return err
	}
	jobCtx, cancel := context.WithTimeout(ctx, TargetedWorkerSmokeTimeout)
	defer cancel()
	if _, err := runtime.Jail().Run(jobCtx, command); err != nil {
		return fmt.Errorf("run targeted smoke job on worker %s: %w", worker.Name, err)
	}
	return nil
}

func targetedWorkerSmokeCommand(worker WorkerInfo) (string, error) {
	if worker.Name == "" {
		return "", fmt.Errorf("build targeted worker smoke command: worker name is empty")
	}

	if worker.HasGPU {
		return fmt.Sprintf("timeout %.0f srun -w %s --gpus-per-node=1 nvidia-smi -L >/dev/null",
			targetedWorkerSmokeCommandTimeout.Seconds(), ShellQuote(worker.Name)), nil
	}
	return fmt.Sprintf("timeout %.0f srun -w %s hostname",
		targetedWorkerSmokeCommandTimeout.Seconds(), ShellQuote(worker.Name)), nil
}
