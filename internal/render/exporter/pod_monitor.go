package exporter

import (
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderPodMonitor(
	clusterName, namespace string,
	exporterValues values.SlurmExporter,
) prometheusv1.PodMonitor {
	pmConfig := exporterValues.PodMonitorConfig
	metricRelabelConfigs := getDefaultMetricRelabelConfigs()
	metricRelabelConfigs = append(metricRelabelConfigs, pmConfig.MetricRelabelConfigs...)

	return prometheusv1.PodMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
			Labels: map[string]string{
				consts.LabelNameKey: consts.LabelNameExporterValue,
			},
		},
		Spec: prometheusv1.PodMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					consts.LabelComponentKey: consts.Exporter,
				},
			},
			JobLabel: pmConfig.JobLabel,
			PodMetricsEndpoints: []prometheusv1.PodMetricsEndpoint{
				{
					Interval:             pmConfig.Interval,
					ScrapeTimeout:        pmConfig.ScrapeTimeout,
					Path:                 consts.ContainerPathExporter,
					Port:                 ptr.To(consts.ContainerPortNameExporter),
					Scheme:               ptr.To(prometheusv1.Scheme("http")),
					MetricRelabelConfigs: metricRelabelConfigs,
					RelabelConfigs:       pmConfig.RelabelConfig,
				},
				{
					Interval:             pmConfig.Interval,
					ScrapeTimeout:        pmConfig.ScrapeTimeout,
					Path:                 consts.ContainerPathMonitoring,
					Port:                 ptr.To(consts.ContainerPortNameMonitoring),
					Scheme:               ptr.To(prometheusv1.Scheme("http")),
					MetricRelabelConfigs: metricRelabelConfigs,
					RelabelConfigs:       pmConfig.RelabelConfig,
				},
			},
		},
	}
}

// getDefaultMetricRelabelConfigs returns the default metric relabel configs to drop
// 'pod', 'instance', and 'container' labels which are added by Kubernetes service discovery
func getDefaultMetricRelabelConfigs() []prometheusv1.RelabelConfig {
	return []prometheusv1.RelabelConfig{
		{
			Action: "labeldrop",
			Regex:  "pod|instance|container",
		},
	}
}
