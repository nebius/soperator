package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

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

// waitForJobRequeuedAfterPrologFailure waits for a job whose Prolog check failed to be requeued: with
// nohold_on_prolog_fail it returns to PENDING with a restart, and pinned to the drained worker it stays there.
func waitForJobRequeuedAfterPrologFailure(ctx context.Context, exec framework.Runtime, slurm *framework.SlurmClient, job framework.SbatchJob, timeout time.Duration) error {
	var latest framework.SlurmJobInfo
	err := exec.WaitFor(ctx, fmt.Sprintf("Slurm job %s requeued after a Prolog failure", job.ID), timeout, framework.DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		info, err := slurm.JobInfo(waitCtx, job.ID)
		if err != nil {
			return false, err
		}
		latest = info
		if info.CompletedSuccessfully() {
			return false, &framework.PermanentError{Err: fmt.Errorf("job %s completed successfully although the Prolog check should have failed", job.ID)}
		}
		return info.QueueState == "PENDING" && info.Restarts != nil && *info.Restarts >= 1, nil
	})
	if err != nil {
		return framework.AnnotateWithJobLog(ctx, exec, slurm, job,
			fmt.Errorf("%w; latest: state=%q restarts=%s sacct=%q", err, latest.QueueState, formatRestarts(latest.Restarts), latest.SacctDump))
	}
	return nil
}

func formatRestarts(restarts *int) string {
	if restarts == nil {
		return "unknown"
	}
	return fmt.Sprint(*restarts)
}
