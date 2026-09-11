package artifacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"gopkg.in/yaml.v3"
)

const fluxNamespace = "flux-system"

type fluxCDCollector struct {
	kubectl framework.ArgsScope
}

// NewFluxCDCollector creates a collector for redacted FluxCD configuration and status.
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
	if err := c.collectRedactedResource(ctx, destination, "helmreleases"); err != nil {
		failures = append(failures, err)
	}

	for _, resource := range []string{"kustomizations", "gitrepositories", "ocirepositories", "helmrepositories"} {
		if err := c.collectRedactedResource(ctx, destination, resource); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (c *fluxCDCollector) collectRedactedResource(ctx context.Context, destination, resource string) error {
	output, err := c.kubectl.Run(ctx, "get", resource, "-n", fluxNamespace, "-o", "json")
	if err != nil {
		return fmt.Errorf("get FluxCD %s: %w", resource, err)
	}
	var value any
	if err := json.Unmarshal([]byte(output), &value); err != nil {
		return fmt.Errorf("decode FluxCD %s: %w", resource, err)
	}
	encoded, err := yaml.Marshal(redactValue(value))
	if err != nil {
		return fmt.Errorf("encode redacted FluxCD %s: %w", resource, err)
	}
	return writeArtifact(destination, resource+".yaml", string(encoded))
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, child := range typed {
			if sensitiveFluxKey(key) {
				redacted[key] = "[redacted]"
				continue
			}
			redacted[key] = redactValue(child)
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for i, child := range typed {
			redacted[i] = redactValue(child)
		}
		return redacted
	default:
		return value
	}
}

func sensitiveFluxKey(key string) bool {
	normalized := strings.NewReplacer("-", "", "_", "", ".", "").Replace(strings.ToLower(key))
	if normalized == "secretref" || normalized == "secretkeyref" {
		return false
	}
	for _, fragment := range []string{"password", "passwd", "token", "privatekey", "accesskey", "credential", "clientsecret"} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return normalized == "secret"
}
