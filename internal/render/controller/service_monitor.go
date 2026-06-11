package controller

import (
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// allNativeMetricsEndpoints lists every slurmctld OpenMetrics subendpoint
// known to this version of soperator. /metrics itself is only an index in
// OpenMetrics-1.0; the actual metrics live under per-category leaves.
//
// "jobs-users-accts" is unbounded (per Slurm's docs); see the field doc on
// OpenMetricsServiceMonitor.Endpoints for the rationale.
var allNativeMetricsEndpoints = []string{
	"jobs",
	"jobs-users-accts",
	"nodes",
	"partitions",
	"scheduler",
}

// ServiceMonitorName returns the name of the ServiceMonitor that scrapes
// slurmctld's native OpenMetrics endpoint for the given cluster.
func ServiceMonitorName(clusterName string) string {
	return clusterName + "-slurmctld-metrics"
}

// RenderServiceMonitor renders a ServiceMonitor that scrapes slurmctld's
// native OpenMetrics endpoint over the controller Service.
func RenderServiceMonitor(
	namespace, clusterName string,
	controller *values.SlurmController,
	cfg slurmv1.OpenMetricsServiceMonitor,
) prometheusv1.ServiceMonitor {
	labels := common.RenderLabels(consts.ComponentTypeController, clusterName)
	selector := common.RenderMatchLabels(consts.ComponentTypeController, clusterName)

	metricRelabel := []prometheusv1.RelabelConfig{{
		Action: "labeldrop",
		Regex:  "pod|instance|container",
	}}

	selected := cfg.Endpoints
	if len(selected) == 0 {
		selected = allNativeMetricsEndpoints
	}
	// Older prometheus-operator releases (still in use in some clusters)
	// accept only the lowercase scheme. Use SchemeHTTP.String() — same pattern
	// as the existing exporter PodMonitor renderer.
	scheme := prometheusv1.SchemeHTTP
	schemeLower := prometheusv1.Scheme((&scheme).String())
	endpoints := make([]prometheusv1.Endpoint, 0, len(selected))
	for _, name := range selected {
		endpoints = append(endpoints, prometheusv1.Endpoint{
			Port:                 controller.ContainerSlurmctld.Name,
			Path:                 "/metrics/" + name,
			Scheme:               ptr.To(schemeLower),
			Interval:             cfg.Interval,
			ScrapeTimeout:        cfg.ScrapeTimeout,
			MetricRelabelConfigs: metricRelabel,
		})
	}

	return prometheusv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceMonitorName(clusterName),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: prometheusv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{MatchLabels: selector},
			NamespaceSelector: prometheusv1.NamespaceSelector{
				MatchNames: []string{namespace},
			},
			Endpoints: endpoints,
		},
	}
}
