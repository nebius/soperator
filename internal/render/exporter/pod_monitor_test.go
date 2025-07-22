package exporter_test

import (
	"testing"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/exporter"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderPodMonitor(t *testing.T) {
	jobLabel := "slurm-exporter-test"
	interval := "1m"
	scrapeTimeout := "30m"

	slurmExporter := values.SlurmExporter{
		Enabled: true,
		PodMonitorConfig: slurmv1.PodMonitorConfig{
			JobLabel:             jobLabel,
			Interval:             prometheusv1.Duration(interval),
			ScrapeTimeout:        prometheusv1.Duration(scrapeTimeout),
			MetricRelabelConfigs: []prometheusv1.RelabelConfig{},
			RelabelConfig:        []prometheusv1.RelabelConfig{},
		},
	}

	expected := &prometheusv1.PodMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultNameCluster,
			Namespace: defaultNamespace,
		},
		Spec: prometheusv1.PodMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					consts.LabelComponentKey: consts.Exporter,
				},
			},
			JobLabel: jobLabel,
			PodMetricsEndpoints: []prometheusv1.PodMetricsEndpoint{
				{
					Interval:      prometheusv1.Duration(interval),
					ScrapeTimeout: prometheusv1.Duration(scrapeTimeout),
					Path:          consts.ContainerPathExporter,
					Port:          ptr.To(consts.ContainerPortNameExporter),
					Scheme:        consts.ContainerSchemeExporter,
				},
			},
		},
	}

	result := exporter.RenderPodMonitor(defaultNameCluster, defaultNamespace, slurmExporter)
	assert.Equal(t, expected.Name, result.Name)
	assert.Equal(t, expected.Namespace, result.Namespace)
	assert.Equal(t, expected.Spec.Selector, result.Spec.Selector)
	assert.Equal(t, expected.Spec.JobLabel, result.Spec.JobLabel)
	assert.Equal(t, expected.Spec.PodMetricsEndpoints[0].Interval, result.Spec.PodMetricsEndpoints[0].Interval)
	assert.Equal(t, expected.Spec.PodMetricsEndpoints[0].ScrapeTimeout, result.Spec.PodMetricsEndpoints[0].ScrapeTimeout)
	assert.Equal(t, expected.Spec.PodMetricsEndpoints[0].Path, result.Spec.PodMetricsEndpoints[0].Path)
	assert.Equal(t, expected.Spec.PodMetricsEndpoints[0].Port, result.Spec.PodMetricsEndpoints[0].Port)
	assert.Equal(t, expected.Spec.PodMetricsEndpoints[0].Scheme, result.Spec.PodMetricsEndpoints[0].Scheme)

	// Verify default MetricRelabelConfigs are added
	assert.Len(t, result.Spec.PodMetricsEndpoints[0].MetricRelabelConfigs, 1)
	assert.Equal(t, "labeldrop", result.Spec.PodMetricsEndpoints[0].MetricRelabelConfigs[0].Action)
	assert.Equal(t, "pod|instance|container", result.Spec.PodMetricsEndpoints[0].MetricRelabelConfigs[0].Regex)
}

func Test_RenderPodMonitor_WithUserMetricRelabelConfigs(t *testing.T) {
	jobLabel := "slurm-exporter-test"
	interval := "1m"
	scrapeTimeout := "30m"

	// User-provided MetricRelabelConfigs
	userConfigs := []prometheusv1.RelabelConfig{
		{
			Action: "replace",
			Regex:  "custom-regex",
		},
	}

	slurmExporter := values.SlurmExporter{
		Enabled: true,
		PodMonitorConfig: slurmv1.PodMonitorConfig{
			JobLabel:             jobLabel,
			Interval:             prometheusv1.Duration(interval),
			ScrapeTimeout:        prometheusv1.Duration(scrapeTimeout),
			MetricRelabelConfigs: userConfigs,
			RelabelConfig:        []prometheusv1.RelabelConfig{},
		},
	}

	result := exporter.RenderPodMonitor(defaultNameCluster, defaultNamespace, slurmExporter)

	// Verify defaults are added first, then user configs
	assert.Len(t, result.Spec.PodMetricsEndpoints[0].MetricRelabelConfigs, 2)

	// Check default config (should come first)
	assert.Equal(t, "labeldrop", result.Spec.PodMetricsEndpoints[0].MetricRelabelConfigs[0].Action)
	assert.Equal(t, "pod|instance|container", result.Spec.PodMetricsEndpoints[0].MetricRelabelConfigs[0].Regex)

	// Check user config (should come last)
	assert.Equal(t, "replace", result.Spec.PodMetricsEndpoints[0].MetricRelabelConfigs[1].Action)
	assert.Equal(t, "custom-regex", result.Spec.PodMetricsEndpoints[0].MetricRelabelConfigs[1].Regex)
}
