package prometheus

import (
	"errors"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	consts "nebius.ai/slurm-operator/internal/consts"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func RenderPodMonitor(
	clusterName, namespace string,
	metrics *slurmv1.Telemetry,
) (*prometheusv1.PodMonitor, error) {
	if metrics == nil || metrics.Prometheus == nil || !metrics.Prometheus.Enabled {
		return nil, errors.New("prometheus PodMonitor is not enabled")
	}

	metricsSpec := metrics.Prometheus.PodMonitorConfig

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
