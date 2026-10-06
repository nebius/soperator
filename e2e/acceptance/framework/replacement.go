package framework

import (
	"context"
	"strings"
	"time"
)

// WaitForInstanceRemoval waits until Compute no longer returns the original instance.
func WaitForInstanceRemoval(ctx context.Context, runtime Runtime, instanceID string, timeout, pollInterval time.Duration) error {
	return runtime.WaitFor(ctx, "old instance removal", timeout, pollInterval, func(waitCtx context.Context) (bool, error) {
		_, err := runtime.Local().Run(waitCtx, "nebius", "compute", "instance", "get", "--id", instanceID, "--format", "json")
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// WaitForUsableReplacement waits for a Slurm worker to become usable on a different instance.
func WaitForUsableReplacement(ctx context.Context, runtime Runtime, slurm *SlurmClient, workerName, originalInstanceID string, timeout, pollInterval time.Duration) error {
	return runtime.WaitFor(ctx, "replacement node ready", timeout, pollInterval, func(waitCtx context.Context) (bool, error) {
		node, err := slurm.NodeInfo(waitCtx, workerName)
		if err != nil {
			return false, err
		}
		if node.InstanceID == "" || node.InstanceID == originalInstanceID || node.State == "" {
			return false, nil
		}
		if !node.IsUsable() {
			return false, nil
		}
		return true, nil
	})
}
