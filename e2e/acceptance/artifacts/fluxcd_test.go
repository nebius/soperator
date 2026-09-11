package artifacts

import (
	"context"
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

func TestFluxCDCollectorCollectsOnlyReconciliationResources(t *testing.T) {
	var calls [][]string
	collector := &fluxCDCollector{kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, args)
		return `{"items":[]}`, nil
	})}
	destination := t.TempDir()

	require.NoError(t, collector.Collect(t.Context(), destination))
	assert.Equal(t, [][]string{
		{"get", "helmreleases", "-n", fluxNamespace, "-o", "wide"},
		{"get", "helmreleases", "-n", fluxNamespace, "-o", "json"},
		{"get", "kustomizations", "-n", fluxNamespace, "-o", "json"},
		{"get", "gitrepositories", "-n", fluxNamespace, "-o", "json"},
		{"get", "ocirepositories", "-n", fluxNamespace, "-o", "json"},
		{"get", "helmrepositories", "-n", fluxNamespace, "-o", "json"},
	}, calls)
	for _, filename := range []string{
		"helmreleases.txt",
		"helmreleases.yaml",
		"kustomizations.yaml",
		"gitrepositories.yaml",
		"ocirepositories.yaml",
		"helmrepositories.yaml",
	} {
		assert.FileExists(t, filepath.Join(destination, filename))
	}
	assert.NoDirExists(t, filepath.Join(destination, "values-from"))
	assert.NoFileExists(t, filepath.Join(destination, "events.txt"))
}
