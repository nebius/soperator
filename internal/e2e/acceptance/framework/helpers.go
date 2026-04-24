package framework

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

const DefaultPollInterval = 10 * time.Second

func RequiredEnv(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("required environment variable %s is not set", key)
	}
	return value, nil
}

func ParseSbatchJobID(output string) (string, error) {
	firstLine := strings.TrimSpace(strings.SplitN(output, "\n", 2)[0])
	if firstLine == "" {
		return "", fmt.Errorf("empty sbatch output")
	}
	if idx := strings.Index(firstLine, ";"); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	jobID := strings.TrimSpace(firstLine)
	if jobID == "" {
		return "", fmt.Errorf("empty job id in sbatch output %q", output)
	}
	return jobID, nil
}

func TreeOutputHasEntries(output string) bool {
	lines := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) < 3 {
		return false
	}

	for _, line := range lines[1 : len(lines)-1] {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

func WaitForTreeEntriesOnWorker(ctx context.Context, exec Exec, worker, storagePath, description string, timeout time.Duration) error {
	trimmedWorker := strings.TrimSpace(worker)
	if trimmedWorker == "" {
		return fmt.Errorf("%s: worker is not selected", description)
	}

	return exec.WaitFor(ctx, description, timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		out, err := exec.Worker(trimmedWorker).RunWithDefaultRetry(waitCtx, fmt.Sprintf("sudo tree -L 2 -a %s", ShellQuote(storagePath)))
		if err != nil {
			return false, err
		}
		return TreeOutputHasEntries(out), nil
	})
}

// WaitForWithJobAlive is Exec.WaitFor with an added short‑circuit on job death. On each tick:
//   - if Slurm still considers the job alive (PENDING / RUNNING / COMPLETING / …), probe runs exactly
//     as it would under a plain Exec.WaitFor; the tick result — ready / not‑ready / error — is passed through;
//   - if the job has left the queue or transitioned to a terminal state (COMPLETED / FAILED / CANCELLED / TIMEOUT / …),
//     the wait aborts immediately with an error carrying the observed state and the sacct dump,
//     so we don't burn the full timeout once the job can no longer possibly satisfy the probe.
func WaitForWithJobAlive(
	ctx context.Context,
	exec Exec,
	slurm *SlurmClient,
	job SbatchJob,
	description string,
	timeout, pollInterval time.Duration,
	probe func(context.Context) (bool, error),
) error {
	if job.IsZero() {
		return fmt.Errorf("%s: job id is empty", description)
	}

	return exec.WaitFor(ctx, description, timeout, pollInterval, func(waitCtx context.Context) (bool, error) {
		state, dump, stateErr := slurm.JobState(waitCtx, job.ID)
		if stateErr != nil {
			return false, fmt.Errorf("check job %s state: %w", job.ID, stateErr)
		}
		if !IsJobAliveState(state) {
			return false, fmt.Errorf("job %s is not alive (state=%q); sacct: %s", job.ID, state, singleLine(dump))
		}
		return probe(waitCtx)
	})
}

// AnnotateWithJobLog decorates err with the job's sacct state and the tail of its
// stdout / stderr files so the godog / GitHub Actions log is self‑contained for triage.
// On nil err it returns nil.
// Tailing is best‑effort; failures to read a log are noted in the returned error instead of shadowing the original.
func AnnotateWithJobLog(ctx context.Context, exec Exec, slurm *SlurmClient, job SbatchJob, err error) error {
	if err == nil {
		return nil
	}
	if job.IsZero() {
		return err
	}

	var extras []string
	if _, dump, stateErr := slurm.JobState(ctx, job.ID); stateErr == nil && dump != "" {
		extras = append(extras, fmt.Sprintf("sacct: %s", singleLine(dump)))
	}
	for _, entry := range []struct{ label, path string }{
		{"stdout", job.StdoutPath},
		{"stderr", job.StderrPath},
	} {
		if strings.TrimSpace(entry.path) == "" {
			continue
		}
		tail, tailErr := exec.Jail().Run(ctx, fmt.Sprintf("tail -n 200 %s 2>&1 || true", ShellQuote(entry.path)))
		trimmed := strings.TrimSpace(tail)
		switch {
		case tailErr != nil:
			extras = append(extras, fmt.Sprintf("%s %s: %v", entry.label, entry.path, tailErr))
		case trimmed == "":
			extras = append(extras, fmt.Sprintf("%s %s: empty", entry.label, entry.path))
		default:
			extras = append(extras, fmt.Sprintf("%s %s:\n%s", entry.label, entry.path, trimmed))
		}
	}

	if len(extras) == 0 {
		return err
	}
	return fmt.Errorf("%w\n%s", err, strings.Join(extras, "\n"))
}

func singleLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
}
