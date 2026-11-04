package artifacts

import (
	"context"
	"errors"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const fluxNamespace = "flux-system"

type fluxCDCollector struct {
	kubectl framework.ArgsScope
}

// NewFluxCDCollector creates a collector for FluxCD configuration and status.
func NewFluxCDCollector(kubectl framework.ArgsScope) Collector {
	return &fluxCDCollector{kubectl: kubectl}
}

func (c *fluxCDCollector) Name() string { return "fluxcd" }

func (c *fluxCDCollector) Collect(ctx context.Context, destination string) error {
	var failures []error
	if err := collectArgs(ctx, destination, "helmreleases.txt", c.kubectl,
		"get", "helmreleases", "-n", fluxNamespace, "-o", "wide"); err != nil {
		failures = append(failures, err)
	}
	if err := collectArgs(ctx, destination, "helmreleases.yaml", c.kubectl,
		"get", "helmreleases", "-n", fluxNamespace, "-o", "yaml"); err != nil {
		failures = append(failures, err)
	}

	for _, resource := range []string{"kustomizations", "gitrepositories", "ocirepositories", "helmrepositories"} {
		if err := collectArgs(ctx, destination, resource+".yaml", c.kubectl,
			"get", resource, "-n", fluxNamespace, "-o", "yaml"); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
