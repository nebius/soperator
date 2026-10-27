package artifacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/validation"
)

type valuesReference struct {
	Kind      string `yaml:"kind"`
	Name      string `yaml:"name"`
	ValuesKey string `yaml:"valuesKey"`
	Optional  bool   `yaml:"optional"`
}

type valuesConfigMap struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Data map[string]string `json:"data"`
}

// collectValuesConfigMaps captures only explicitly referenced ConfigMap data keys.
// Secrets, unreferenced keys and annotations (including last-applied data) stay out.
func (c *fluxCDCollector) collectValuesConfigMaps(ctx context.Context, destination string) error {
	data, err := os.ReadFile(filepath.Join(destination, "helmreleases.yaml"))
	if err != nil {
		return fmt.Errorf("read HelmRelease values references: %w", err)
	}
	var releases struct {
		Items []struct {
			Spec struct {
				ValuesFrom []valuesReference `yaml:"valuesFrom"`
			} `yaml:"spec"`
		} `yaml:"items"`
	}
	if err := yaml.Unmarshal(data, &releases); err != nil {
		return fmt.Errorf("parse HelmRelease values references: %w", err)
	}
	refs := make(map[string]map[string]bool)
	for _, release := range releases.Items {
		for _, ref := range release.Spec.ValuesFrom {
			if ref.Kind != "ConfigMap" {
				continue
			}
			if len(validation.IsDNS1123Subdomain(ref.Name)) != 0 {
				return fmt.Errorf("invalid values ConfigMap name %q", ref.Name)
			}
			key := ref.ValuesKey
			if key == "" {
				key = "values.yaml"
			}
			if refs[ref.Name] == nil {
				refs[ref.Name] = make(map[string]bool)
			}
			optional, exists := refs[ref.Name][key]
			refs[ref.Name][key] = ref.Optional && (!exists || optional)
		}
	}

	var failures []error
	items := make([]valuesConfigMap, 0, len(refs))
	for _, name := range slices.Sorted(maps.Keys(refs)) {
		keys := refs[name]
		optional := true
		for _, value := range keys {
			optional = optional && value
		}
		args := []string{"get", "configmap", name, "-n", fluxNamespace, "-o", "json"}
		if optional {
			args = append(args, "--ignore-not-found")
		}
		output, err := c.kubectl.Run(ctx, args...)
		if err != nil {
			failures = append(failures, fmt.Errorf("get values ConfigMap %s/%s: %w", fluxNamespace, name, err))
			continue
		}
		if optional && strings.TrimSpace(output) == "" {
			continue
		}
		var source valuesConfigMap
		if err := json.Unmarshal([]byte(output), &source); err != nil {
			failures = append(failures, fmt.Errorf("parse values ConfigMap %s/%s: %w", fluxNamespace, name, err))
			continue
		}
		if source.Kind != "ConfigMap" || source.Metadata.Name != name || source.Metadata.Namespace != fluxNamespace {
			failures = append(failures, fmt.Errorf("unexpected values ConfigMap identity for %s/%s", fluxNamespace, name))
			continue
		}
		selected := make(map[string]string, len(keys))
		for _, key := range slices.Sorted(maps.Keys(keys)) {
			value, exists := source.Data[key]
			if !exists {
				failures = append(failures, fmt.Errorf("values ConfigMap %s/%s has no data key %q", fluxNamespace, name, key))
				continue
			}
			selected[key] = value
		}
		source.Data = selected
		items = append(items, source)
	}
	result := struct {
		APIVersion string            `json:"apiVersion"`
		Kind       string            `json:"kind"`
		Items      []valuesConfigMap `json:"items"`
	}{APIVersion: "v1", Kind: "List", Items: items}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		failures = append(failures, fmt.Errorf("encode values ConfigMaps: %w", err))
	} else {
		directory := filepath.Join(destination, "values-from")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			failures = append(failures, fmt.Errorf("create values ConfigMap directory: %w", err))
		} else if err := os.WriteFile(filepath.Join(directory, "configmaps.json"), append(encoded, '\n'), 0o600); err != nil {
			failures = append(failures, fmt.Errorf("write values ConfigMaps: %w", err))
		}
	}
	return errors.Join(failures...)
}
