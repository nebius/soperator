package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	containerSmokeSacctTimeout = 2 * time.Minute

	// Cluster-supported GPU diagnostic image; includes nvidia-smi for container GPU visibility.
	// https://github.com/nebius/ml-containers/pull/98
	gpuSmokeImageRef    = "ml-containers/training_diag:13.0.2-ubuntu24.04-20260819103152"
	gpuSmokeDockerImage = "cr.eu-north1.nebius.cloud/" + gpuSmokeImageRef
	gpuSmokeEnrootImage = "docker://cr.eu-north1.nebius.cloud#" + gpuSmokeImageRef
)

func waitForJobSucceeded(ctx context.Context, exec framework.Exec, slurm *framework.SlurmClient, job framework.SbatchJob, timeout time.Duration) error {
	if err := framework.AnnotateWithJobLog(ctx, exec, slurm, job, slurm.WaitForJobGone(ctx, job.ID, timeout)); err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, containerSmokeSacctTimeout)
	defer cancel()

	for {
		if err := waitCtx.Err(); err != nil {
			return framework.AnnotateWithJobLog(ctx, exec, slurm, job,
				fmt.Errorf("wait for job %s completed successfully: %w", job.ID, err))
		}

		info, err := slurm.JobInfo(waitCtx, job.ID)
		if err != nil {
			return framework.AnnotateWithJobLog(ctx, exec, slurm, job, err)
		}
		if info.SacctFound {
			if info.CompletedSuccessfully() {
				return nil
			}
			if !framework.IsJobAliveState(info.SacctState) {
				return framework.AnnotateWithJobLog(ctx, exec, slurm, job,
					fmt.Errorf("job %s finished with state=%s exit_code=%s", job.ID, info.SacctState, info.SacctExit))
			}
		}

		select {
		case <-waitCtx.Done():
		case <-time.After(framework.DefaultPollInterval):
		}
	}
}

func assertJobStdoutReportsVisibleGPUs(ctx context.Context, exec framework.Exec, job framework.SbatchJob) error {
	stdout, err := readJobFile(ctx, exec, job.StdoutPath)
	if err != nil {
		return err
	}
	return assertGPUListing(stdout, fmt.Sprintf("job stdout %s", job.StdoutPath))
}

func assertGPUListing(output, source string) error {
	if strings.Contains(output, "GPU ") {
		return nil
	}
	return fmt.Errorf("expected nvidia-smi GPU listing in %s, got output:\n%s", source, strings.TrimSpace(output))
}

func readJobFile(ctx context.Context, exec framework.Exec, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("job output path is empty")
	}
	output, err := exec.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf("cat %s", framework.ShellQuote(path)))
	if err != nil {
		return "", fmt.Errorf("read job output %s: %w", path, err)
	}
	return output, nil
}
