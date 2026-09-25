package artifacts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	jailCopyAttempts = 3
	jailCopyDelay    = 5 * time.Second
)

type jailCollector struct {
	kubectl framework.ArgsScope
}

// NewJailCollector creates a collector for shared and node-local jail files.
func NewJailCollector(kubectl framework.ArgsScope) Collector {
	return &jailCollector{kubectl: kubectl}
}

func (c *jailCollector) Name() string { return "jail" }

func (c *jailCollector) Collect(ctx context.Context, destination string) error {
	var failures []error
	pod, err := c.runningPod(ctx, "app.kubernetes.io/component=sconfigcontroller")
	if err != nil {
		failures = append(failures, err)
	} else {
		if err := c.collectSharedJail(ctx, destination, pod); err != nil {
			failures = append(failures, err)
		}
	}

	workers, err := c.runningPods(ctx, "slurm.nebius.ai/worker=true")
	if err != nil {
		failures = append(failures, err)
	}
	for _, worker := range workers {
		workerDestination := filepath.Join(destination, "workers", sanitizeCollectorName(worker))
		if err := c.copyFromPod(ctx, worker, "slurmd", "/mnt/jail.upper/opt/soperator-outputs/local/.", workerDestination); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (c *jailCollector) collectSharedJail(ctx context.Context, destination, pod string) error {
	var failures []error
	if err := collectArgs(ctx, destination, "tree.txt", c.kubectl,
		"exec", "-n", framework.SoperatorNamespace, pod, "--",
		"sh", "-lc", "find /mnt/jail -maxdepth 6 -not -path '*/proc/*' 2>&1 || true"); err != nil {
		failures = append(failures, err)
	}
	for _, copy := range []struct {
		source      string
		destination string
	}{
		{"/mnt/jail/etc/slurm/.", "etc/slurm"},
		{"/mnt/jail/opt/soperator-outputs/.", "opt/soperator-outputs"},
	} {
		if err := c.copyFromPod(ctx, pod, "", copy.source, filepath.Join(destination, copy.destination)); err != nil {
			failures = append(failures, err)
		}
	}

	return errors.Join(failures...)
}

func (c *jailCollector) runningPod(ctx context.Context, selector string) (string, error) {
	pods, err := c.runningPods(ctx, selector)
	if err != nil {
		return "", err
	}
	if len(pods) == 0 {
		return "", fmt.Errorf("find running pod with selector %q: no pods found", selector)
	}
	return pods[0], nil
}

func (c *jailCollector) runningPods(ctx context.Context, selector string) ([]string, error) {
	if c.kubectl == nil {
		return nil, fmt.Errorf("find running pods with selector %q: kubectl scope is unavailable", selector)
	}
	output, err := c.kubectl.Run(ctx,
		"get", "pods", "-n", framework.SoperatorNamespace,
		"-l", selector, "--field-selector=status.phase=Running",
		"-o", "jsonpath={.items[*].metadata.name}",
	)
	if err != nil {
		return nil, fmt.Errorf("find running pods with selector %q: %w", selector, err)
	}
	return strings.Fields(output), nil
}

func (c *jailCollector) copyFromPod(ctx context.Context, pod, container, source, destination string) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("create jail artifact directory %q: %w", destination, err)
	}
	args := []string{"cp"}
	if container != "" {
		args = append(args, "-c", container)
	}
	args = append(args, fmt.Sprintf("%s/%s:%s", framework.SoperatorNamespace, pod, source), destination)
	if _, err := c.kubectl.RunWithRetry(ctx, jailCopyAttempts, jailCopyDelay, args...); err != nil {
		return fmt.Errorf("copy %s from pod %s: %w", source, pod, err)
	}
	return nil
}
