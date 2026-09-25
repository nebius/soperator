package artifacts

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type kubernetesCollector struct {
	kubectl framework.ArgsScope
}

// NewKubernetesCollector creates a collector for general Kubernetes state.
func NewKubernetesCollector(kubectl framework.ArgsScope) Collector {
	return &kubernetesCollector{kubectl: kubectl}
}

func (c *kubernetesCollector) Name() string { return "kubernetes" }

func (c *kubernetesCollector) Collect(ctx context.Context, destination string) error {
	commands := []struct {
		filename string
		args     []string
	}{
		{"pods.txt", []string{"get", "pods", "-A", "-o", "wide"}},
		{"events.txt", []string{"get", "events", "-A", "--sort-by=.lastTimestamp"}},
		{"nodes.txt", []string{"get", "nodes", "-o", "wide"}},
		{"nodes.yaml", []string{"get", "nodes", "-o", "yaml"}},
		{"jobs.txt", []string{"get", "jobs", "-n", framework.SoperatorNamespace, "-o", "wide"}},
		{"jobs.yaml", []string{"get", "jobs", "-n", framework.SoperatorNamespace, "-o", "yaml"}},
	}

	var failures []error
	for _, command := range commands {
		if err := collectArgs(ctx, destination, command.filename, c.kubectl, command.args...); err != nil {
			failures = append(failures, err)
		}
	}

	dumpDir := filepath.Join(destination, "cluster-info")
	if err := collectArgs(ctx, destination, "cluster-info-command.txt", c.kubectl,
		"cluster-info", "dump",
		"--namespaces=kruise-system,soperator-system,soperator,flux-system,cert-manager-system",
		fmt.Sprintf("--output-directory=%s", dumpDir),
	); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}
