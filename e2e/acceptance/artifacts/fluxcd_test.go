package artifacts

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

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
		{"get", "helmreleases", "-n", fluxNamespace, "-o", "yaml"},
		{"get", "kustomizations", "-n", fluxNamespace, "-o", "yaml"},
		{"get", "gitrepositories", "-n", fluxNamespace, "-o", "yaml"},
		{"get", "ocirepositories", "-n", fluxNamespace, "-o", "yaml"},
		{"get", "helmrepositories", "-n", fluxNamespace, "-o", "yaml"},
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
