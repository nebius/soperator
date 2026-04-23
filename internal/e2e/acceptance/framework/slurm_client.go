package framework

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

type SlurmClient struct {
	exec Exec
}

func NewSlurmClient(exec Exec) *SlurmClient {
	return &SlurmClient{exec: exec}
}

func (s *SlurmClient) AnyWorkers(count int) ([]string, error) {
	return pickAnyWorkerNames(s.exec.AvailableWorkers(), count, "workers")
}

func (s *SlurmClient) AnyGPUWorkers(count int) ([]string, error) {
	return pickAnyWorkerNames(s.exec.AvailableGPUWorkers(), count, "GPU workers")
}

func (s *SlurmClient) WaitForJobRunning(ctx context.Context, jobID string, timeout time.Duration) error {
	return s.exec.WaitFor(ctx, fmt.Sprintf("job %s running", jobID), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		status, err := s.exec.Jail().RunWithDefaultRetry(waitCtx, fmt.Sprintf("squeue -h -j %s -o '%%T'", ShellQuote(jobID)))
		if err != nil {
			return false, err
		}
		return strings.Contains(status, "RUNNING"), nil
	})
}

func (s *SlurmClient) WaitForJobGone(ctx context.Context, jobID string, timeout time.Duration) error {
	return s.exec.WaitFor(ctx, fmt.Sprintf("job %s gone from queue", jobID), timeout, DefaultPollInterval, func(waitCtx context.Context) (bool, error) {
		status, err := s.exec.Jail().RunWithDefaultRetry(waitCtx, fmt.Sprintf("squeue -h -j %s -o '%%T'", ShellQuote(jobID)))
		if err != nil {
			return false, err
		}
		return strings.TrimSpace(status) == "", nil
	})
}

func (s *SlurmClient) CancelJob(ctx context.Context, jobID string, waitTimeout time.Duration) error {
	if strings.TrimSpace(jobID) == "" {
		return nil
	}

	if _, err := s.exec.Jail().RunWithDefaultRetry(ctx, fmt.Sprintf("scancel %s", ShellQuote(jobID))); err != nil {
		if !isMissingJobError(err) {
			return fmt.Errorf("scancel job %s: %w", jobID, err)
		}
	}

	if waitTimeout <= 0 {
		return nil
	}
	if err := s.WaitForJobGone(ctx, jobID, waitTimeout); err != nil {
		return fmt.Errorf("wait for job %s to finish: %w", jobID, err)
	}
	return nil
}

func pickAnyWorkerNames(pool []WorkerPodRef, count int, label string) ([]string, error) {
	if count < 1 {
		return nil, fmt.Errorf("invalid %s count %d", label, count)
	}
	if len(pool) < count {
		return nil, fmt.Errorf("found %d %s, need %d", len(pool), label, count)
	}

	indices := rand.Perm(len(pool))[:count]
	out := make([]string, 0, count)
	for _, i := range indices {
		out = append(out, pool[i].Name)
	}
	return out, nil
}

func isMissingJobError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid job id") || strings.Contains(message, "does not exist")
}
