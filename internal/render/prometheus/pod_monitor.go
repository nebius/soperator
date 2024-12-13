package prometheus

import (
	"errors"

	consts "nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func RenderPodMonitor(
	clusterName, namespace string,
	exporterValues *values.SlurmExporter,
) (*prometheusv1.PodMonitor, error) {
	if exporterValues == nil || !exporterValues.Enabled {
		return nil, errors.New("prometheus PodMonitor is not enabled")
	}

	metricsSpec := exporterValues.PodMonitorConfig

	return &prometheusv1.PodMonitor{
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
			JobLabel: metricsSpec.JobLabel,
			PodMetricsEndpoints: []prometheusv1.PodMetricsEndpoint{
				{
					Interval:             metricsSpec.Interval,
					ScrapeTimeout:        metricsSpec.ScrapeTimeout,
					Path:                 consts.ContainerPathExporter,
					Port:                 consts.ContainerPortNameExporter,
					Scheme:               consts.ContainerSchemeExporter,
					MetricRelabelConfigs: metricsSpec.MetricRelabelConfigs,
					RelabelConfigs:       metricsSpec.RelabelConfig,
				},
			},
		},
	}, nil
}
