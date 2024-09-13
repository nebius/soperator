package prometheus_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	consts "nebius.ai/slurm-operator/internal/consts"
	slurmprometheus "nebius.ai/slurm-operator/internal/render/prometheus"
	"nebius.ai/slurm-operator/internal/values"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

func Test_RenderPodMonitor(t *testing.T) {
	jobLabel := "slurm-exporter-test"
	interval := "1m"
	scrapeTimeout := "30m"

	//telemetry := &slurmv1.Telemetry{
	//	Prometheus: &slurmv1.MetricsPrometheus{
	//		Enabled: true,
	//		PodMonitorConfig: slurmv1.PodMonitorConfig{
	//			JobLabel:             jobLabel,
	//			Interval:             prometheusv1.Duration(interval),
	//			ScrapeTimeout:        prometheusv1.Duration(scrapeTimeout),
	//			MetricRelabelConfigs: []prometheusv1.RelabelConfig{},
	//			RelabelConfig:        []prometheusv1.RelabelConfig{},
	//		},
	//	},
	//}

	exporter := values.SlurmExporter{
		//SlurmNode:         slurmv1.SlurmNode{},
		//Name:              "",
		//ExporterContainer: slurmv1.ExporterContainer{},
		//ContainerMunge:    values.Container{},
		//VolumeJail:        slurmv1.NodeVolume{},
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
					Port:          consts.ContainerPortNameExporter,
					Scheme:        consts.ContainerSchemeExporter,
				},
			},
		},
	}

	result, err := slurmprometheus.RenderPodMonitor(defaultNameCluster, defaultNamespace, &exporter)
	assert.NoError(t, err)
	assert.Equal(t, expected.Name, result.Name)
	assert.Equal(t, expected.Namespace, result.Namespace)
	assert.Equal(t, expected.Spec.Selector, result.Spec.Selector)
	assert.Equal(t, expected.Spec.JobLabel, result.Spec.JobLabel)
	assert.Equal(t, expected.Spec.PodMetricsEndpoints[0].Interval, result.Spec.PodMetricsEndpoints[0].Interval)
	assert.Equal(t, expected.Spec.PodMetricsEndpoints[0].ScrapeTimeout, result.Spec.PodMetricsEndpoints[0].ScrapeTimeout)
	assert.Equal(t, expected.Spec.PodMetricsEndpoints[0].Path, result.Spec.PodMetricsEndpoints[0].Path)
	assert.Equal(t, expected.Spec.PodMetricsEndpoints[0].Port, result.Spec.PodMetricsEndpoints[0].Port)
	assert.Equal(t, expected.Spec.PodMetricsEndpoints[0].Scheme, result.Spec.PodMetricsEndpoints[0].Scheme)
}
