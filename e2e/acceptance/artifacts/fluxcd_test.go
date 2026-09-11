package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestRedactValuePreservesSecretReferences(t *testing.T) {
	input := map[string]any{
		"password": "plain-text-password",
		"apiToken": "plain-text-token",
		"secretRef": map[string]any{
			"name": "values-secret",
			"key":  "values.yaml",
		},
		"nested": map[string]any{"client-secret": "plain-text-client-secret"},
	}

	redacted, ok := redactValue(input).(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "[redacted]", redacted["password"])
	assert.Equal(t, "[redacted]", redacted["apiToken"])
	assert.Equal(t, map[string]any{"name": "values-secret", "key": "values.yaml"}, redacted["secretRef"])
	assert.Equal(t, map[string]any{"client-secret": "[redacted]"}, redacted["nested"])
}

func TestCollectValuesConfigMapsSkipsMissingOptionalReferences(t *testing.T) {
	var calls int
	collector := &fluxCDCollector{kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		calls++
		assert.Equal(t, []string{"get", "configmaps", "-n", "flux-system", "-o", "json"}, args)
		return `{"items":[{"metadata":{"name":"present"},"data":{"values.yaml":"password: unsafe\nreplicas: 2"}}]}`, nil
	})}
	destination := t.TempDir()

	err := collector.collectValuesConfigMaps(t.Context(), destination, []fluxValuesReference{
		{Namespace: "flux-system", Kind: "ConfigMap", Name: "present"},
		{Namespace: "flux-system", Kind: "ConfigMap", Name: "absent", Optional: true},
		{Namespace: "flux-system", Kind: "Secret", Name: "never-read"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, calls)

	content, err := os.ReadFile(filepath.Join(destination, "values-from", "flux-system-present.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(content), "unsafe")
	assert.Contains(t, string(content), "[redacted]")
}

func TestHelmValuesReferencesIncludeConfigMapsAndSecrets(t *testing.T) {
	references := helmValuesReferences([]map[string]any{
		{
			"metadata": map[string]any{"name": "soperator", "namespace": "flux-system"},
			"spec": map[string]any{"valuesFrom": []any{
				map[string]any{"kind": "Secret", "name": "private-values", "valuesKey": "values.yaml"},
				map[string]any{"kind": "ConfigMap", "name": "public-values", "targetPath": "workers"},
				map[string]any{"name": "default-secret"},
			}},
		},
	})

	require.Len(t, references, 3)
	assert.Equal(t, "Secret", references[0].Kind)
	assert.Equal(t, "default-secret", references[0].Name)
	assert.Equal(t, "Secret", references[1].Kind)
	assert.Equal(t, "private-values", references[1].Name)
	assert.Equal(t, "ConfigMap", references[2].Kind)
	assert.Equal(t, "public-values", references[2].Name)
}
