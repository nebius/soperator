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
)

const soperatorNamespace = "soperator"

func (w *world) Logf(format string, args ...any) {
	w.logf(format, args...)
}

func (w *world) ExecController(ctx context.Context, command string) (string, error) {
	return w.Run(ctx, "kubectl", "exec", "-n", soperatorNamespace, "controller-0", "--", "bash", "-lc", command)
}

func (w *world) ExecControllerWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error) {
	return w.RunWithRetry(ctx, attempts, delay,
		"kubectl", "exec", "-n", soperatorNamespace, "controller-0", "--", "bash", "-lc", command)
}

func (w *world) ExecJail(ctx context.Context, command string) (string, error) {
	return w.Run(ctx, "kubectl", "exec", "-n", soperatorNamespace, "login-0", "--", "chroot", "/mnt/jail", "bash", "-lc", command)
}

func (w *world) ExecJailWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error) {
	return w.RunWithRetry(ctx, attempts, delay,
		"kubectl", "exec", "-n", soperatorNamespace, "login-0", "--", "chroot", "/mnt/jail", "bash", "-lc", command)
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
