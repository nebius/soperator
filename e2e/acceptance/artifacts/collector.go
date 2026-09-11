// Package artifacts collects reusable diagnostics from Soperator E2E environments.
package artifacts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

var invalidCollectorName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Collector writes one category of diagnostics to a caller-owned directory.
type Collector interface {
	Name() string
	Collect(ctx context.Context, destination string) error
}

// CollectAll runs every collector and returns their combined failures.
func CollectAll(ctx context.Context, root string, collectors ...Collector) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return fmt.Errorf("artifact output directory is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create artifact output directory %q: %w", root, err)
	}

	seen := make(map[string]struct{}, len(collectors))
	var failures []error
	for i, collector := range collectors {
		if collector == nil {
			failures = append(failures, fmt.Errorf("collector %d is nil", i))
			continue
		}
		name := sanitizeCollectorName(collector.Name())
		if _, ok := seen[name]; ok {
			failures = append(failures, fmt.Errorf("collector name %q is duplicated", name))
			continue
		}
		seen[name] = struct{}{}

		destination := filepath.Join(root, name)
		if err := os.MkdirAll(destination, 0o755); err != nil {
			failures = append(failures, fmt.Errorf("create collector %q directory: %w", name, err))
			continue
		}
		if err := collector.Collect(ctx, destination); err != nil {
			wrappedErr := fmt.Errorf("collect %s artifacts: %w", name, err)
			failures = append(failures, wrappedErr)
			if writeErr := os.WriteFile(filepath.Join(destination, "_error.txt"), []byte(wrappedErr.Error()+"\n"), 0o600); writeErr != nil {
				failures = append(failures, fmt.Errorf("write collector %q error: %w", name, writeErr))
			}
		}
	}
	return errors.Join(failures...)
}

// CommonCollectors returns the shared cluster diagnostics.
func CommonCollectors(runtime framework.Runtime, nebiusProjectID string) []Collector {
	return []Collector{
		NewKubernetesCollector(runtime.Kubectl()),
		NewSoperatorCollector(runtime.Kubectl()),
		NewFluxCDCollector(runtime.Kubectl()),
		NewSlurmCollector(runtime.Controller()),
		NewJailCollector(runtime.Kubectl()),
		NewMK8sCollector(runtime.Local(), nebiusProjectID),
	}
}

func sanitizeCollectorName(name string) string {
	name = invalidCollectorName.ReplaceAllString(strings.TrimSpace(name), "-")
	name = strings.Trim(name, "-._")
	if name == "" {
		return "unknown"
	}
	return name
}
