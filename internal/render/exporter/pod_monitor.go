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
					Scheme:               consts.ContainerSchemeExporter,
					MetricRelabelConfigs: pmConfig.MetricRelabelConfigs,
					RelabelConfigs:       pmConfig.RelabelConfig,
				},
			},
		},
	}
}
