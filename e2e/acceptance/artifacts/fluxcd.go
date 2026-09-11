package artifacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"gopkg.in/yaml.v3"
)

const fluxNamespace = "flux-system"

type fluxCDCollector struct {
	kubectl framework.ArgsScope
}

type fluxObjectList struct {
	Items []map[string]any `json:"items"`
}

type fluxValuesReference struct {
	Release    string `yaml:"release"`
	Namespace  string `yaml:"namespace"`
	Kind       string `yaml:"kind"`
	Name       string `yaml:"name"`
	ValuesKey  string `yaml:"valuesKey,omitempty"`
	TargetPath string `yaml:"targetPath,omitempty"`
	Optional   bool   `yaml:"optional"`
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
	if err := c.collectHelmReleases(ctx, destination); err != nil {
		failures = append(failures, err)
	}

	for _, resource := range []string{"kustomizations", "gitrepositories", "ocirepositories", "helmrepositories"} {
		if err := c.collectRedactedResource(ctx, destination, resource); err != nil {
			failures = append(failures, err)
		}
	}
	if err := collectArgs(ctx, destination, "events.txt", c.kubectl,
		"get", "events", "-n", fluxNamespace, "--sort-by=.lastTimestamp"); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (c *fluxCDCollector) collectHelmReleases(ctx context.Context, destination string) error {
	output, err := c.kubectl.Run(ctx, "get", "helmreleases", "-n", fluxNamespace, "-o", "json")
	if err != nil {
		return fmt.Errorf("get FluxCD HelmReleases: %w", err)
	}

	var releases fluxObjectList
	if err := json.Unmarshal([]byte(output), &releases); err != nil {
		return fmt.Errorf("decode FluxCD HelmReleases: %w", err)
	}
	var releaseDocument any
	if err := json.Unmarshal([]byte(output), &releaseDocument); err != nil {
		return fmt.Errorf("decode FluxCD HelmRelease document: %w", err)
	}
	sanitized, err := yaml.Marshal(redactValue(releaseDocument))
	if err != nil {
		return fmt.Errorf("encode redacted FluxCD HelmReleases: %w", err)
	}
	if err := writeArtifact(destination, "helmreleases.yaml", string(sanitized)); err != nil {
		return err
	}

	references := helmValuesReferences(releases.Items)
	referenceYAML, err := yaml.Marshal(references)
	if err != nil {
		return fmt.Errorf("encode FluxCD values references: %w", err)
	}
	if err := writeArtifact(destination, "values-from/references.yaml", string(referenceYAML)); err != nil {
		return err
	}

	return c.collectValuesConfigMaps(ctx, destination, references)
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

func (c *fluxCDCollector) collectValuesConfigMaps(
	ctx context.Context,
	destination string,
	references []fluxValuesReference,
) error {
	byNamespace := make(map[string][]fluxValuesReference)
	for _, reference := range references {
		if strings.EqualFold(reference.Kind, "ConfigMap") {
			byNamespace[reference.Namespace] = append(byNamespace[reference.Namespace], reference)
		}
	}

	var failures []error
	for namespace, namespaceReferences := range byNamespace {
		output, err := c.kubectl.Run(ctx, "get", "configmaps", "-n", namespace, "-o", "json")
		if err != nil {
			failures = append(failures, fmt.Errorf("list FluxCD values ConfigMaps in %s: %w", namespace, err))
			continue
		}
		var configMaps fluxObjectList
		if err := json.Unmarshal([]byte(output), &configMaps); err != nil {
			failures = append(failures, fmt.Errorf("decode FluxCD values ConfigMaps in %s: %w", namespace, err))
			continue
		}
		byName := make(map[string]map[string]any, len(configMaps.Items))
		for _, configMap := range configMaps.Items {
			metadata, _ := configMap["metadata"].(map[string]any)
			name, _ := metadata["name"].(string)
			byName[name] = configMap
		}

		seen := make(map[string]struct{})
		for _, reference := range namespaceReferences {
			if _, ok := seen[reference.Name]; ok {
				continue
			}
			seen[reference.Name] = struct{}{}
			configMap, ok := byName[reference.Name]
			if !ok {
				if !reference.Optional {
					failures = append(failures, fmt.Errorf("find required FluxCD values ConfigMap %s/%s", namespace, reference.Name))
				}
				continue
			}
			if err := writeValuesConfigMap(destination, namespace, reference.Name, configMap); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func writeValuesConfigMap(destination, namespace, name string, configMap map[string]any) error {
	delete(configMap, "binaryData")
	if data, ok := configMap["data"].(map[string]any); ok {
		for key, raw := range data {
			text, ok := raw.(string)
			if !ok {
				data[key] = redactValue(raw)
				continue
			}
			var decoded any
			if err := yaml.Unmarshal([]byte(text), &decoded); err != nil {
				data[key] = "[unparseable value omitted]"
				continue
			}
			data[key] = redactValue(decoded)
		}
	}
	encoded, err := yaml.Marshal(redactValue(configMap))
	if err != nil {
		return fmt.Errorf("encode FluxCD values ConfigMap %s/%s: %w", namespace, name, err)
	}
	filename := filepath.Join("values-from", sanitizeCollectorName(namespace+"-"+name)+".yaml")
	return writeArtifact(destination, filename, string(encoded))
}

func helmValuesReferences(releases []map[string]any) []fluxValuesReference {
	var references []fluxValuesReference
	for _, release := range releases {
		metadata, _ := release["metadata"].(map[string]any)
		spec, _ := release["spec"].(map[string]any)
		valuesFrom, _ := spec["valuesFrom"].([]any)
		releaseName, _ := metadata["name"].(string)
		namespace, _ := metadata["namespace"].(string)
		if namespace == "" {
			namespace = fluxNamespace
		}
		for _, raw := range valuesFrom {
			value, _ := raw.(map[string]any)
			reference := fluxValuesReference{
				Release:    releaseName,
				Namespace:  namespace,
				Kind:       stringValue(value["kind"], "Secret"),
				Name:       stringValue(value["name"], ""),
				ValuesKey:  stringValue(value["valuesKey"], ""),
				TargetPath: stringValue(value["targetPath"], ""),
				Optional:   boolValue(value["optional"]),
			}
			if reference.Name != "" {
				references = append(references, reference)
			}
		}
	}
	sort.Slice(references, func(i, j int) bool {
		left := references[i].Namespace + "/" + references[i].Name + "/" + references[i].Release
		right := references[j].Namespace + "/" + references[j].Name + "/" + references[j].Release
		return left < right
	})
	return references
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

func stringValue(value any, fallback string) string {
	if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
		return text
	}
	return fallback
}

func boolValue(value any) bool {
	boolean, _ := value.(bool)
	return boolean
}
