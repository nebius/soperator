package acceptance

import (
	"bytes"
	"context"
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

func (w *world) ExecJail(ctx context.Context, command string) (string, error) {
	return w.Run(ctx, "kubectl", "exec", "-n", soperatorNamespace, "login-0", "--", "chroot", "/mnt/jail", "bash", "-lc", command)
}

func (w *world) ExecJailWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error) {
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		out, err := w.ExecJail(ctx, command)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if attempt == attempts {
			break
		}

		w.logf("retrying SSH command after attempt %d/%d: %v", attempt, attempts, err)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(delay):
		}
	}

	return "", lastErr
}

func (w *world) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, w.commandTimeout)
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

func (w *world) WaitFor(ctx context.Context, description string, timeout, pollInterval time.Duration, condition func(context.Context) (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		done, err := condition(ctx)
		if err == nil && done {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("wait for %s: %w", description, err)
			}
			return fmt.Errorf("wait for %s: timed out after %s", description, timeout)
		}
		if err != nil {
			w.logf("wait for %s still pending: %v", description, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
