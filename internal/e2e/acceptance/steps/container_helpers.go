package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const containerPollInterval = 10 * time.Second

func selectGPUWorkers(ctx context.Context, exec framework.Exec, count int) ([]string, error) {
	if count < 1 {
		return nil, fmt.Errorf("invalid GPU worker count %d", count)
	}

	output, err := framework.ExecJailWithDefaultRetry(ctx, exec, `sinfo -hN -p main -o '%N %G'`)
	if err != nil {
		return nil, fmt.Errorf("discover GPU workers from Slurm: %w", err)
	}

	seen := make(map[string]struct{})
	workers := make([]string, 0, count)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		gres := strings.ToLower(strings.Join(fields[1:], " "))
		if !strings.Contains(gres, "gpu") {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		workers = append(workers, name)
		if len(workers) == count {
			return workers, nil
		}
	}

	if len(workers) < count {
		return nil, fmt.Errorf("found %d GPU workers in Slurm, need %d", len(workers), count)
	}
	return workers[:count], nil
}

func parseSbatchJobID(output string) (string, error) {
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

func waitForJobRunning(ctx context.Context, exec framework.Exec, jobID string, timeout time.Duration) error {
	return exec.WaitFor(ctx, fmt.Sprintf("job %s running", jobID), timeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
		status, err := framework.ExecJailWithDefaultRetry(waitCtx, exec, fmt.Sprintf("squeue -h -j %s -o '%%T'", framework.ShellQuote(jobID)))
		if err != nil {
			return false, err
		}
		return strings.Contains(status, "RUNNING"), nil
	})
}

func waitForJobGone(ctx context.Context, exec framework.Exec, jobID string, timeout time.Duration) error {
	return exec.WaitFor(ctx, fmt.Sprintf("job %s gone from queue", jobID), timeout, containerPollInterval, func(waitCtx context.Context) (bool, error) {
		status, err := framework.ExecJailWithDefaultRetry(waitCtx, exec, fmt.Sprintf("squeue -h -j %s -o '%%T'", framework.ShellQuote(jobID)))
		if err != nil {
			return false, err
		}
		return strings.TrimSpace(status) == "", nil
	})
}

func cancelSlurmJob(ctx context.Context, exec framework.Exec, jobID string, waitTimeout time.Duration) error {
	if strings.TrimSpace(jobID) == "" {
		return nil
	}

	if _, err := exec.ExecJail(ctx, fmt.Sprintf("scancel %s", framework.ShellQuote(jobID))); err != nil {
		// The job may already be gone by the time we try to cancel it.
		if !isMissingJobError(err) {
			return fmt.Errorf("scancel job %s: %w", jobID, err)
		}
	}

	if waitTimeout <= 0 {
		return nil
	}
	if err := waitForJobGone(ctx, exec, jobID, waitTimeout); err != nil {
		return fmt.Errorf("wait for job %s to finish: %w", jobID, err)
	}
	return nil
}

func runWorkerCommand(ctx context.Context, exec framework.Exec, worker, command string) (string, error) {
	return exec.ExecJail(ctx, fmt.Sprintf("ssh %s %s", framework.ShellQuote(worker), framework.ShellQuote(command)))
}

func runWorkerCommandWithDefaultRetry(ctx context.Context, exec framework.Exec, worker, command string) (string, error) {
	return framework.ExecJailWithDefaultRetry(ctx, exec, fmt.Sprintf("ssh %s %s", framework.ShellQuote(worker), framework.ShellQuote(command)))
}

func treeOutputHasEntries(output string) bool {
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

func isMissingJobError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid job id") || strings.Contains(message, "does not exist")
}
