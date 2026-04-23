package framework

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

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

	return exec.WaitFor(ctx, description, timeout, SlurmPollInterval, func(waitCtx context.Context) (bool, error) {
		out, err := exec.Worker(trimmedWorker).RunWithDefaultRetry(waitCtx, fmt.Sprintf("sudo tree -L 2 -a %s", ShellQuote(storagePath)))
		if err != nil {
			return false, err
		}
		return TreeOutputHasEntries(out), nil
	})
}
