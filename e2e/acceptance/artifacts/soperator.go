package artifacts

import (
	"context"
	"errors"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type soperatorCollector struct {
	kubectl framework.ArgsScope
}

// NewSoperatorCollector creates a collector for Soperator custom resources.
func NewSoperatorCollector(kubectl framework.ArgsScope) Collector {
	return &soperatorCollector{kubectl: kubectl}
}

func (c *soperatorCollector) Name() string { return "soperator" }

func (c *soperatorCollector) Collect(ctx context.Context, destination string) error {
	resources := []string{"slurmclusters", "nodesets", "activechecks", "jailedconfigs"}
	var failures []error
	for _, resource := range resources {
		if err := collectArgs(ctx, destination, resource+".txt", c.kubectl, "get", resource, "-A", "-o", "wide"); err != nil {
			failures = append(failures, err)
		}
		if err := collectArgs(ctx, destination, resource+".yaml", c.kubectl, "get", resource, "-A", "-o", "yaml"); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
