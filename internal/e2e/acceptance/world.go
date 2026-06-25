package acceptance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const (
	commandTimeout     = 10 * time.Minute
	soperatorNamespace = "soperator"
)

type world struct {
	logPrefix string

	state *framework.ClusterState
}

func (w *world) AvailableWorkers() []framework.WorkerPodRef {
	return append([]framework.WorkerPodRef(nil), w.state.Workers...)
}

func (w *world) AvailableGPUWorkers() []framework.WorkerPodRef {
	return append([]framework.WorkerPodRef(nil), w.state.GPUWorkers...)
}

func (w *world) Logf(format string, args ...any) {
	w.logf(format, args...)
}

func (w *world) Controller() framework.CommandScope {
	return controllerScope{world: w}
}

func (w *world) Jail() framework.CommandScope {
	return jailScope{world: w}
}

func (w *world) Worker(worker string) framework.CommandScope {
	return workerScope{world: w, worker: worker}
}

func (w *world) RunWithDefaultRetry(ctx context.Context, name string, args ...string) (string, error) {
	return w.RunWithRetry(ctx, framework.DefaultRetryAttempts, framework.DefaultRetryDelay, name, args...)
}

func (w *world) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	w.logf("run: %s %s", name, strings.Join(args, " "))

	cmd := exec.CommandContext(cmdCtx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := stdout.String()
	errOut := strings.TrimSpace(stderr.String())
	if err != nil {
		if errOut != "" {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, errOut)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}

	if errOut != "" {
		log.Printf("%s: stderr: %s", w.logPrefix, errOut)
	}

	return out, nil
}

func (w *world) RunWithRetry(ctx context.Context, attempts int, delay time.Duration, name string, args ...string) (string, error) {
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	var out string
	for attempt := 1; attempt <= attempts; attempt++ {
		out, lastErr = w.Run(ctx, name, args...)
		if lastErr == nil {
			return out, nil
		}
		if attempt == attempts {
			break
		}

		w.logf("retrying command after attempt %d/%d: %s %s: %v",
			attempt, attempts, name, strings.Join(args, " "), lastErr)
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(delay):
		}
	}

	return out, lastErr
}

type controllerScope struct {
	world *world
}

func (s controllerScope) Run(ctx context.Context, command string) (string, error) {
	return s.world.Run(ctx, "kubectl", "exec", "-n", soperatorNamespace, "controller-0", "--", "bash", "-lc", command)
}

func (s controllerScope) RunWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error) {
	return s.world.RunWithRetry(ctx, attempts, delay, "kubectl", "exec", "-n", soperatorNamespace, "controller-0", "--", "bash", "-lc", command)
}

func (s controllerScope) RunWithDefaultRetry(ctx context.Context, command string) (string, error) {
	return s.RunWithRetry(ctx, command, framework.DefaultRetryAttempts, framework.DefaultRetryDelay)
}

type jailScope struct {
	world *world
}

func (s jailScope) Run(ctx context.Context, command string) (string, error) {
	return s.world.Run(ctx, "kubectl", "exec", "-n", soperatorNamespace, "login-0", "--", "chroot", "/mnt/jail", "bash", "-lc", command)
}

func (s jailScope) RunWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error) {
	return s.world.RunWithRetry(ctx, attempts, delay, "kubectl", "exec", "-n", soperatorNamespace, "login-0", "--", "chroot", "/mnt/jail", "bash", "-lc", command)
}

func (s jailScope) RunWithDefaultRetry(ctx context.Context, command string) (string, error) {
	return s.RunWithRetry(ctx, command, framework.DefaultRetryAttempts, framework.DefaultRetryDelay)
}

type workerScope struct {
	world  *world
	worker string
}

func (s workerScope) Run(ctx context.Context, command string) (string, error) {
	return s.world.Jail().Run(ctx, fmt.Sprintf("ssh %s %s", framework.ShellQuote(s.worker), framework.ShellQuote(command)))
}

func (s workerScope) RunWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error) {
	return s.world.Jail().RunWithRetry(ctx, fmt.Sprintf("ssh %s %s", framework.ShellQuote(s.worker), framework.ShellQuote(command)), attempts, delay)
}

func (s workerScope) RunWithDefaultRetry(ctx context.Context, command string) (string, error) {
	return s.RunWithRetry(ctx, command, framework.DefaultRetryAttempts, framework.DefaultRetryDelay)
}

func (w *world) WaitFor(ctx context.Context, description string, timeout, pollInterval time.Duration, condition func(context.Context) (bool, error)) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for {
		if err := waitCtx.Err(); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				if lastErr != nil {
					return fmt.Errorf("wait for %s: %w", description, lastErr)
				}
				return fmt.Errorf("wait for %s: timed out after %s", description, timeout)
			}
			return err
		}

		done, err := condition(waitCtx)
		if err == nil && done {
			return nil
		}
		if err != nil && waitCtx.Err() == nil {
			lastErr = err
			w.logf("wait for %s still pending: %v", description, err)
		}

		select {
		case <-waitCtx.Done():
		case <-time.After(pollInterval):
		}
	}
}
