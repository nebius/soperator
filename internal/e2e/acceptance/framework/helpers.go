//go:build acceptance

package framework

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
)

const soperatorNamespace = "soperator"

type Executor struct {
	commandTimeout time.Duration
}

func NewExecutor(commandTimeout time.Duration) *Executor {
	return &Executor{commandTimeout: commandTimeout}
}

func (e *Executor) ExecController(ctx context.Context, command string) (string, error) {
	return e.Run(ctx, "kubectl", "exec", "-n", soperatorNamespace, "controller-0", "--", "bash", "-lc", command)
}

func (e *Executor) ExecJail(ctx context.Context, command string) (string, error) {
	return e.Run(ctx, "kubectl", "exec", "-n", soperatorNamespace, "login-0", "--", "chroot", "/mnt/jail", "bash", "-lc", command)
}

func (e *Executor) ExecJailWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error) {
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		out, err := e.ExecJail(ctx, command)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if attempt == attempts {
			break
		}

		e.Logf("retrying jail command after attempt %d/%d: %v", attempt, attempts, err)

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(delay):
		}
	}

	return "", lastErr
}

func (e *Executor) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, e.commandTimeout)
	defer cancel()

	e.Logf("run: %s %s", name, strings.Join(args, " "))

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
		e.Logf("stderr: %s", errOut)
	}

	return out, nil
}

func (e *Executor) Logf(format string, args ...any) {
	fmt.Fprintf(GinkgoWriter, "acceptance: "+format+"\n", args...)
}

func ShellQuote(value string) string {
	result := "'"
	for _, r := range value {
		if r == '\'' {
			result += `'"'"'`
			continue
		}
		result += string(r)
	}
	result += "'"

	return result
}
