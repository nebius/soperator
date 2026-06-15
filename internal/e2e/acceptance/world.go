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

	state          *framework.ClusterState
	kubectlContext string
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

func (w *world) Kubectl() framework.ArgsScope {
	return argsScope{
		run: func(ctx context.Context, args ...string) (string, error) {
			return w.Run(ctx, "kubectl", kubectlArgs(w.kubectlContext, args)...)
		},
	}
}

func (w *world) Local() framework.ArgsScope {
	return argsScope{
		run: func(ctx context.Context, args ...string) (string, error) {
			if len(args) == 0 {
				return "", fmt.Errorf("local command requires executable name")
			}
			return w.Run(ctx, args[0], args[1:]...)
		},
	}
}

func (w *world) Controller() framework.CommandScope {
	return commandScope{
		run: func(ctx context.Context, command string) (string, error) {
			return w.Kubectl().Run(ctx, "exec", "-n", soperatorNamespace, w.state.SlurmClusterName+"-controller-0", "--", "bash", "-lc", command)
		},
	}
}

func (w *world) Jail() framework.CommandScope {
	return commandScope{
		run: func(ctx context.Context, command string) (string, error) {
			return w.Kubectl().Run(ctx, "exec", "-n", soperatorNamespace, w.state.SlurmClusterName+"-login-0", "--", "chroot", "/mnt/jail", "bash", "-lc", command)
		},
	}
}

func (w *world) Worker(worker string) framework.CommandScope {
	return commandScope{
		run: func(ctx context.Context, command string) (string, error) {
			return w.Jail().Run(ctx, fmt.Sprintf("ssh %s %s", framework.ShellQuote(worker), framework.ShellQuote(command)))
		},
	}
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

func kubectlArgs(kubectlContext string, args []string) []string {
	if kubectlContext == "" {
		return append([]string(nil), args...)
	}

	out := make([]string, 0, len(args)+2)
	out = append(out, "--context", kubectlContext)
	out = append(out, args...)
	return out
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
