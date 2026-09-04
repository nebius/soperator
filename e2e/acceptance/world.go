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

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type world struct {
	logPrefix string

	kubectlContext   string
	slurmClusterName string
	soperatorVersion string
}

func (w *world) Logf(format string, args ...any) {
	w.logf(format, args...)
}

func (w *world) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		return w.Run(ctx, "kubectl", kubectlArgs(w.kubectlContext, args)...)
	})
}

func (w *world) Local() framework.ArgsScope {
	return framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		if len(args) == 0 {
			return "", fmt.Errorf("local command requires executable name")
		}
		return w.Run(ctx, args[0], args[1:]...)
	})
}

func (w *world) Controller() framework.CommandScope {
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		return w.Kubectl().Run(ctx, "exec", "-n", framework.SoperatorNamespace, framework.SoperatorPodName(w.slurmClusterName, w.soperatorVersion, "controller-0"), "--", "bash", "-lc", command)
	})
}

func (w *world) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		return w.Kubectl().Run(ctx, "exec", "-n", framework.SoperatorNamespace, framework.SoperatorPodName(w.slurmClusterName, w.soperatorVersion, "login-0"), "--", "chroot", "/mnt/jail", "bash", "-lc", command)
	})
}

func (w *world) Worker(worker framework.WorkerInfo) framework.CommandScope {
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if strings.TrimSpace(worker.Name) == "" {
			return "", fmt.Errorf("Slurm worker name is empty")
		}
		return w.Jail().Run(ctx, fmt.Sprintf("ssh %s %s", framework.ShellQuote(worker.Name), framework.ShellQuote(command)))
	})
}

func (w *world) WorkerPod(pod framework.WorkerPodInfo) framework.CommandScope {
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if strings.TrimSpace(pod.PodName) == "" {
			return "", fmt.Errorf("Kubernetes worker pod for Slurm node %s is empty", pod.SlurmNodeName)
		}
		return w.Kubectl().Run(ctx,
			"exec", "-n", framework.SoperatorNamespace, pod.PodName, "-c", "slurmd",
			"--", "bash", "-lc", command,
		)
	})
}

func (w *world) Run(ctx context.Context, name string, args ...string) (string, error) {
	w.logf("run: %s %s", name, strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, name, args...)
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
