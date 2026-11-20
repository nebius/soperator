package acceptance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	commandTimeout     = 10 * time.Minute
	workloadPodTimeout = 2 * time.Minute
)

type world struct {
	logPrefix string

	kubectlContext   string
	slurmClusterName string

	podNamesMu        sync.Mutex
	controllerPodName string
	loginPodName      string
}

// NewLocalArgsScope creates a local process scope without requiring a Kubernetes runtime.
func NewLocalArgsScope() framework.ArgsScope {
	return (&world{logPrefix: "acceptance"}).Local()
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
	return w.workloadPodScope("controller", &w.controllerPodName, "bash", "-lc")
}

func (w *world) Jail() framework.CommandScope {
	return w.workloadPodScope("login", &w.loginPodName, "chroot", "/mnt/jail", "bash", "-lc")
}

func (w *world) workloadPodScope(component string, cachedPodName *string, commandPrefix ...string) framework.CommandScope {
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		var output string
		var commandErr error
		var completed bool
		err := w.WaitFor(ctx, "ready "+component+" pod for exec", workloadPodTimeout, framework.DefaultPollInterval,
			func(waitCtx context.Context) (bool, error) {
				podName, err := w.readyWorkloadPodName(waitCtx, component, cachedPodName)
				if err != nil {
					return false, err
				}
				args := append([]string{"exec", "-n", framework.SoperatorNamespace, podName, "--"}, commandPrefix...)
				args = append(args, command)
				// Readiness recovery must not shorten the command's own execution deadline.
				output, commandErr = w.Kubectl().Run(ctx, args...)
				if !framework.KubectlExecNotStarted(output, commandErr) {
					completed = true
					return true, nil
				}
				w.podNamesMu.Lock()
				if *cachedPodName == podName {
					*cachedPodName = ""
				}
				w.podNamesMu.Unlock()
				return false, commandErr
			})
		if completed && ctx.Err() == nil {
			return output, commandErr
		}
		return output, err
	})
}

func (w *world) readyWorkloadPodName(ctx context.Context, component string, cachedPodName *string) (string, error) {
	w.podNamesMu.Lock()
	defer w.podNamesMu.Unlock()

	if *cachedPodName != "" {
		return *cachedPodName, nil
	}

	podName, err := framework.NewKubectlClient(w).ReadyWorkloadPodName(ctx, w.slurmClusterName, component)
	if err != nil {
		return "", err
	}
	*cachedPodName = podName
	return podName, nil
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
		return out, &framework.CommandError{Name: name, Args: args, Err: err, Stderr: errOut}
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
		if waitCtx.Err() != nil {
			continue
		}
		if err == nil && done {
			return nil
		}
		if err != nil {
			var permanent *framework.PermanentError
			if errors.As(err, &permanent) {
				return fmt.Errorf("wait for %s: %w", description, err)
			}
			lastErr = err
			w.logf("wait for %s still pending: %v", description, err)
		}

		select {
		case <-waitCtx.Done():
		case <-time.After(pollInterval):
		}
	}
}
