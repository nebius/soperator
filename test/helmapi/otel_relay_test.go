//go:build helmapi

package helmapi

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestOTelCollectorRelay(t *testing.T) {
	chart := os.Getenv("OTEL_COLLECTOR_CHART")
	if chart == "" {
		t.Skip("set OTEL_COLLECTOR_CHART to the cached opentelemetry-collector 0.149.0 chart")
	}
	_, err := os.Stat(chart)
	require.NoError(t, err, "read local collector chart")
	var metadata struct{ Name, Version string }
	require.NoError(t, yaml.Unmarshal(commandOutput(t, "helm", "show", "chart", chart), &metadata))
	require.Equal(t, "opentelemetry-collector", metadata.Name)
	require.Equal(t, "0.149.0", metadata.Version)

	for _, collector := range []struct {
		key, suffix, pipeline, localExporter string
	}{
		{"logs", "logs", "logs", "otlp_http/victoriametrics"},
		{"jailLogs", "jail-logs", "logs", "otlp_http/victorialogs"},
		{"events", "events", "logs/events", "otlp_http/victoriametrics"},
	} {
		for _, public := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/public=%t", collector.suffix, public), func(t *testing.T) {
				release := "opentelemetry-collector-" + collector.suffix
				prefix := "observability.opentelemetry." + collector.key
				flags := []string{
					"--show-only", "templates/" + release + ".yaml",
					"--set", fmt.Sprintf("observability.publicEndpointEnabled=%t", public),
				}
				exporters := []string{collector.localExporter}
				if public {
					exporters = append(exporters, "otlp")
					flags = append(flags,
						"--set-json", prefix+`.valuesFrom=[
							{"kind":"ConfigMap","name":"terraform-collector","valuesKey":"collector-values.yaml"},
							{"kind":"ConfigMap","name":"site-collector","valuesKey":"values.yaml"}
						]`,
						"--set-json", fmt.Sprintf(`%s.values.config.service.pipelines.%s.exporters=["%s","otlp"]`, prefix, collector.pipeline, collector.localExporter))
				} else {
					flags = append(flags, "--set-json", prefix+".valuesFrom=[]")
				}

				umbrella := renderChart(t, "soperator-fluxcd", "flux-system", "1.36.0", flags...)
				relay := renderCollectorRelay(t, chart, release, umbrella)
				require.NotContains(t, relay.Receivers, "zipkin")
				if collector.key == "jailLogs" {
					for _, disabled := range []string{"otlp", "jaeger", "prometheus"} {
						require.NotContains(t, relay.Receivers, disabled)
					}
				}
				require.NotContains(t, relay.Service.Pipelines, "metrics")
				require.NotContains(t, relay.Service.Pipelines, "traces")
				require.Equal(t, exporters, relay.Service.Pipelines[collector.pipeline].Exporters)
				for name, pipeline := range relay.Service.Pipelines {
					for _, receiver := range pipeline.Receivers {
						require.Contains(t, relay.Receivers, receiver, name)
					}
					for _, processor := range pipeline.Processors {
						require.Contains(t, relay.Processors, processor, name)
					}
					for _, exporter := range pipeline.Exporters {
						require.Contains(t, relay.Exporters, exporter, name)
					}
				}
				if public {
					require.Equal(t, "site.invalid:4317", relay.Exporters["otlp"]["endpoint"])
					require.Equal(t, "gzip", relay.Exporters["otlp"]["compression"])
				}
			})
		}
	}
}

type collectorRelay struct {
	Receivers  map[string]any
	Exporters  map[string]map[string]any
	Processors map[string]any
	Service    struct {
		Pipelines map[string]struct{ Receivers, Processors, Exporters []string }
	}
}

func renderCollectorRelay(t *testing.T, chart, release string, umbrella []manifest) collectorRelay {
	t.Helper()
	var refs []any
	sources := map[string]map[string]string{
		"terraform-collector": {"collector-values.yaml": "config: {exporters: {otlp: {endpoint: discovered.invalid:4317, compression: gzip}}}"},
		"site-collector":      {"values.yaml": "config: {exporters: {otlp: {endpoint: site.invalid:4317}}}"},
	}
	for _, m := range umbrella {
		switch m.object.GetKind() {
		case "HelmRelease":
			var err error
			refs, _, err = unstructured.NestedSlice(m.object.Object, "spec", "valuesFrom")
			require.NoError(t, err)
		case "ConfigMap":
			data, _, err := unstructured.NestedStringMap(m.object.Object, "data")
			require.NoError(t, err)
			sources[m.object.GetName()] = data
		}
	}
	require.NotEmpty(t, refs)
	args := []string{"template", release, chart, "--namespace", "logs-system", "--kube-version", "1.36.0"}
	dir := t.TempDir()
	for i, entry := range refs {
		ref := entry.(map[string]any)
		data := sources[ref["name"].(string)][ref["valuesKey"].(string)]
		require.NotEmpty(t, data, "valuesFrom[%d]: %v", i, ref)
		file := filepath.Join(dir, fmt.Sprintf("%d.yaml", i))
		require.NoError(t, os.WriteFile(file, []byte(data), 0o600))
		args = append(args, "--values", file)
	}

	workloads, err := decodeManifests(commandOutput(t, "helm", args...))
	require.NoError(t, err)
	for _, m := range workloads {
		if m.object.GetKind() == "ConfigMap" {
			config, found, err := unstructured.NestedString(m.object.Object, "data", "relay")
			require.NoError(t, err)
			if found {
				var relay collectorRelay
				require.NoError(t, yaml.Unmarshal([]byte(config), &relay))
				return relay
			}
		}
	}
	t.Fatal("child chart did not render a relay ConfigMap")
	return collectorRelay{}
}
