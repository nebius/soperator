package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const jobSacctTimeout = 2 * time.Minute

func waitForJobSucceeded(ctx context.Context, exec framework.Exec, slurm *framework.SlurmClient, job framework.SbatchJob, timeout time.Duration) error {
	if err := framework.AnnotateWithJobLog(ctx, exec, slurm, job, slurm.WaitForJobGone(ctx, job.ID, timeout)); err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, jobSacctTimeout)
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

func waitForExpectedPrologFailure(
	ctx context.Context,
	exec framework.Exec,
	slurm *framework.SlurmClient,
	job framework.SbatchJob,
	timeout time.Duration,
	reason string,
) (framework.SlurmJobInfo, error) {
	if err := framework.AnnotateWithJobLog(ctx, exec, slurm, job, slurm.WaitForJobGone(ctx, job.ID, timeout)); err != nil {
		return framework.SlurmJobInfo{}, err
	}

	info, err := slurm.JobInfo(ctx, job.ID)
	if err != nil {
		return framework.SlurmJobInfo{}, framework.AnnotateWithJobLog(ctx, exec, slurm, job, err)
	}
	if err := validateExpectedPrologFailure(job.ID, reason, info); err != nil {
		return framework.SlurmJobInfo{}, framework.AnnotateWithJobLog(ctx, exec, slurm, job, err)
	}
	return info, nil
}

func validateExpectedPrologFailure(jobID, reason string, info framework.SlurmJobInfo) error {
	if !info.SacctFound {
		return fmt.Errorf("expected Slurm job %s to be recorded in sacct after Prolog failure, got:\n%s", jobID, info.SacctDump)
	}
	if info.CompletedSuccessfully() {
		return fmt.Errorf("expected Slurm job %s to fail in Prolog due to %s, got state=%s exit_code=%s",
			jobID, reason, info.SacctState, info.SacctExit)
	}
	if info.IsAlive() {
		return fmt.Errorf("expected Slurm job %s to be finished after leaving squeue, got state=%s exit_code=%s",
			jobID, info.SacctState, info.SacctExit)
	}
	return nil
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
