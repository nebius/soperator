package artifacts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestValuesConfigMapsCaptureOnlyReferencedKeys(t *testing.T) {
	destination := t.TempDir()
	releases := `{"items":[{"spec":{"valuesFrom":[
 {"kind":"ConfigMap","name":"shared"},
 {"kind":"Secret","name":"credentials"},
 {"name":"implicit-secret"}
 ]}},{"spec":{"valuesFrom":[
 {"kind":"ConfigMap","name":"shared","valuesKey":"override.yaml"},
 {"kind":"ConfigMap","name":"shared","optional":true}
 ]}}]}`
	require.NoError(t, writeArtifact(destination, "helmreleases.yaml", releases))
	var calls [][]string
	collector := &fluxCDCollector{kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, args)
		return `{"apiVersion":"v1","kind":"ConfigMap","metadata":{
 "name":"shared","namespace":"flux-system","annotations":{"last-applied":"sensitive-annotation"}},
 "data":{"values.yaml":"enabled: true\n","override.yaml":"replicas: 2\n","unused":"sensitive-unused"},
 "binaryData":{"unused":"c2VjcmV0"}}`, nil
	})}
	require.NoError(t, collector.collectValuesConfigMaps(t.Context(), destination))
	assert.Equal(t, [][]string{{"get", "configmap", "shared", "-n", fluxNamespace, "-o", "json"}}, calls)
	data, err := os.ReadFile(filepath.Join(destination, "values-from", "configmaps.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"apiVersion":"v1","kind":"List","items":[{
 "apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"shared","namespace":"flux-system"},
 "data":{"values.yaml":"enabled: true\n","override.yaml":"replicas: 2\n"}}]}`, string(data))
	assert.NotContains(t, string(data), "sensitive")
	assert.NotContains(t, string(data), "binaryData")
}

func TestValuesConfigMapOptionalAndRequiredFailures(t *testing.T) {
	for _, tt := range []struct {
		name, output, wantError string
		optional                bool
		commandError            error
	}{
		{name: "optional missing", optional: true},
		{name: "required missing", commandError: fmt.Errorf("not found"), wantError: "get values ConfigMap flux-system/values"},
		{name: "optional forbidden", optional: true, commandError: fmt.Errorf("forbidden"), wantError: "forbidden"},
		{name: "missing data key", optional: true, output: `{"kind":"ConfigMap","metadata":{"name":"values","namespace":"flux-system"}}`, wantError: `has no data key "values.yaml"`},
		{name: "wrong identity", output: `{"kind":"Secret","metadata":{"name":"values","namespace":"flux-system"}}`, wantError: "unexpected values ConfigMap identity"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			destination := t.TempDir()
			releases := fmt.Sprintf(`{"items":[{"spec":{"valuesFrom":[{"kind":"ConfigMap","name":"values","optional":%t}]}}]}`, tt.optional)
			require.NoError(t, writeArtifact(destination, "helmreleases.yaml", releases))
			collector := &fluxCDCollector{kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
				assert.Equal(t, "configmap", args[1])
				assert.Equal(t, tt.optional, args[len(args)-1] == "--ignore-not-found")
				return tt.output, tt.commandError
			})}
			err := collector.collectValuesConfigMaps(t.Context(), destination)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
				data, err := os.ReadFile(filepath.Join(destination, "values-from", "configmaps.json"))
				require.NoError(t, err)
				var list struct {
					Items []valuesConfigMap `json:"items"`
				}
				require.NoError(t, json.Unmarshal(data, &list))
				assert.Empty(t, list.Items)
			}
		})
	}
}

func TestValuesConfigMapRejectsInvalidNamesBeforeCommands(t *testing.T) {
	destination := t.TempDir()
	require.NoError(t, writeArtifact(destination, "helmreleases.yaml", `{"items":[{"spec":{"valuesFrom":[{"kind":"ConfigMap","name":"--all-namespaces"}]}}]}`))
	collector := &fluxCDCollector{kubectl: framework.NewArgsScope(func(_ context.Context, _ ...string) (string, error) {
		t.Fatal("invalid reference must not run kubectl")
		return "", nil
	})}
	require.ErrorContains(t, collector.collectValuesConfigMaps(t.Context(), destination), "invalid values ConfigMap name")
}
