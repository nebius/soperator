package exporter

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitoringMetrics_Register(t *testing.T) {
	metrics := NewMonitoringMetrics()
	registry := prometheus.NewRegistry()

	err := metrics.Register(registry)
	assert.NoError(t, err)

	// Verify all metrics are registered
	metricFamilies, err := registry.Gather()
	require.NoError(t, err)

	expectedMetrics := map[string]bool{
		"slurm_exporter_collection_duration_seconds": false,
		"slurm_exporter_collection_attempts_total":   false,
		"slurm_exporter_collection_failures_total":   false,
		"slurm_exporter_metrics_requests_total":      false,
		"slurm_exporter_metrics_exported":            false,
	}

	for _, mf := range metricFamilies {
		if _, ok := expectedMetrics[*mf.Name]; ok {
			expectedMetrics[*mf.Name] = true
		}
	}

	for metric, found := range expectedMetrics {
		assert.True(t, found, "Expected metric %s not found", metric)
	}
}

func TestMonitoringMetrics_RecordCollection(t *testing.T) {
	metrics := NewMonitoringMetrics()
	registry := prometheus.NewRegistry()
	require.NoError(t, metrics.Register(registry))

	// Test successful collection
	metrics.RecordCollection(1.5, nil)

	// Test failed collection
	metrics.RecordCollection(0.5, errors.New("collection failed"))

	// Gather metrics
	metricFamilies, err := registry.Gather()
	require.NoError(t, err)

	var attemptsTotal, failuresTotal, durationValue float64

	for _, mf := range metricFamilies {
		switch *mf.Name {
		case "slurm_exporter_collection_attempts_total":
			attemptsTotal = *mf.Metric[0].Counter.Value
		case "slurm_exporter_collection_failures_total":
			failuresTotal = *mf.Metric[0].Counter.Value
		case "slurm_exporter_collection_duration_seconds":
			durationValue = *mf.Metric[0].Gauge.Value
		}
	}

	assert.Equal(t, float64(2), attemptsTotal, "Expected 2 collection attempts")
	assert.Equal(t, float64(1), failuresTotal, "Expected 1 collection failure")
	assert.Equal(t, float64(0.5), durationValue, "Expected duration to be set to the last collection (0.5s)")
}

func TestMonitoringMetrics_RecordMetricsRequest(t *testing.T) {
	metrics := NewMonitoringMetrics()
	registry := prometheus.NewRegistry()
	require.NoError(t, metrics.Register(registry))

	// Record multiple requests
	metrics.RecordMetricsRequest()
	metrics.RecordMetricsRequest()
	metrics.RecordMetricsRequest()

	// Gather metrics
	metricFamilies, err := registry.Gather()
	require.NoError(t, err)

	var requestsTotal float64
	for _, mf := range metricFamilies {
		if *mf.Name == "slurm_exporter_metrics_requests_total" {
			requestsTotal = *mf.Metric[0].Counter.Value
			break
		}
	}

	assert.Equal(t, float64(3), requestsTotal, "Expected 3 metrics requests")
}

func TestMonitoringMetrics_RecordMetricsExported(t *testing.T) {
	metrics := NewMonitoringMetrics()
	registry := prometheus.NewRegistry()
	require.NoError(t, metrics.Register(registry))

	// Record exported metrics count
	metrics.RecordMetricsExported(150)
	metrics.RecordMetricsExported(200) // Should override previous value

	// Gather metrics
	metricFamilies, err := registry.Gather()
	require.NoError(t, err)

	var exportedCount float64
	for _, mf := range metricFamilies {
		if *mf.Name == "slurm_exporter_metrics_exported" {
			exportedCount = *mf.Metric[0].Gauge.Value
			break
		}
	}

	assert.Equal(t, float64(200), exportedCount, "Expected 200 exported metrics")
}

func getMetricValue(families []*dto.MetricFamily, name string, metricType string) float64 {
	for _, mf := range families {
		if *mf.Name == name && len(mf.Metric) > 0 {
			metric := mf.Metric[0]
			switch metricType {
			case "counter":
				return *metric.Counter.Value
			case "gauge":
				return *metric.Gauge.Value
			}
		}
	}
	return 0
}
