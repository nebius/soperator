package framework

import (
	"context"
	"fmt"
	"time"
)

const jobSacctTimeout = 2 * time.Minute

// WaitForJobSucceeded waits up to timeout for the job to leave the active queue,
// then allows up to two additional minutes for its accounting record to become
// terminal. Success requires COMPLETED with exit code 0:0. Failures include the
// available accounting record and job output. The caller's context bounds both waits.
func WaitForJobSucceeded(ctx context.Context, exec Exec, slurm *SlurmClient, job SbatchJob, timeout time.Duration) error {
	if err := AnnotateWithJobLog(ctx, exec, slurm, job, slurm.WaitForJobGone(ctx, job.ID, timeout)); err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, jobSacctTimeout)
	defer cancel()

	for {
		if err := waitCtx.Err(); err != nil {
			return AnnotateWithJobLog(ctx, exec, slurm, job,
				fmt.Errorf("wait for job %s completed successfully: %w", job.ID, err))
		}

		info, err := slurm.JobStatus(waitCtx, job.ID)
		if err != nil {
			return AnnotateWithJobLog(ctx, exec, slurm, job, err)
		}
		if info.SacctFound {
			if info.CompletedSuccessfully() {
				return nil
			}
			if !IsJobAliveState(info.SacctState) {
				return AnnotateWithJobLog(ctx, exec, slurm, job,
					fmt.Errorf("job %s finished with state=%s exit_code=%s", job.ID, info.SacctState, info.SacctExit))
			}
		}

		select {
		case <-waitCtx.Done():
		case <-time.After(DefaultPollInterval):
		}
	}
}
