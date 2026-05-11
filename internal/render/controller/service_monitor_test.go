package controller

import (
	"testing"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func newTestController() *values.SlurmController {
	return &values.SlurmController{
		ContainerSlurmctld: values.Container{
			NodeContainer: slurmv1.NodeContainer{Port: 6817},
			Name:          consts.ContainerNameSlurmctld,
		},
	}
}

func TestRenderServiceMonitor_DefaultEndpoints(t *testing.T) {
	cfg := slurmv1.OpenMetricsServiceMonitor{
		Interval:      "30s",
		ScrapeTimeout: "28s",
	}

	got := RenderServiceMonitor("ns", "cl", newTestController(), cfg)

	assert.Equal(t, ServiceMonitorName("cl"), got.Name)
	assert.Equal(t, "ns", got.Namespace)
	assert.Equal(t, []string{"ns"}, got.Spec.NamespaceSelector.MatchNames)
	assert.Empty(t, got.Spec.JobLabel, "JobLabel should not be set; job= falls back to Service name")
	assert.Equal(t, consts.ComponentTypeController.String(), got.Spec.Selector.MatchLabels[consts.LabelComponentKey])

	if assert.Len(t, got.Spec.Endpoints, len(allNativeMetricsEndpoints)) {
		for i, ep := range got.Spec.Endpoints {
			assert.Equal(t, "/metrics/"+allNativeMetricsEndpoints[i], ep.Path, "endpoint %d path", i)
			assert.Equal(t, consts.ContainerNameSlurmctld, ep.Port, "endpoint %d port", i)
			assert.Equal(t, ptr.To(prometheusv1.Scheme("http")), ep.Scheme, "endpoint %d scheme (lowercase per prom-operator schema)", i)
			assert.Equal(t, prometheusv1.Duration("30s"), ep.Interval, "endpoint %d interval", i)
			assert.Equal(t, prometheusv1.Duration("28s"), ep.ScrapeTimeout, "endpoint %d timeout", i)
			if assert.Len(t, ep.MetricRelabelConfigs, 1, "endpoint %d default labeldrop", i) {
				assert.Equal(t, "labeldrop", ep.MetricRelabelConfigs[0].Action)
				assert.Equal(t, "pod|instance|container", ep.MetricRelabelConfigs[0].Regex)
			}
		}
	}
}

func TestRenderServiceMonitor_ExplicitEndpointSubset(t *testing.T) {
	cfg := slurmv1.OpenMetricsServiceMonitor{
		Interval:      "30s",
		ScrapeTimeout: "28s",
		Endpoints:     []string{"jobs", "scheduler"},
	}

	got := RenderServiceMonitor("ns", "cl", newTestController(), cfg)

	if assert.Len(t, got.Spec.Endpoints, 2) {
		assert.Equal(t, "/metrics/jobs", got.Spec.Endpoints[0].Path)
		assert.Equal(t, "/metrics/scheduler", got.Spec.Endpoints[1].Path)
	}
}
