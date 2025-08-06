package exporter

import (
	"github.com/prometheus/client_golang/prometheus"
)

// MonitoringMetrics contains self-monitoring metrics for the exporter
type MonitoringMetrics struct {
	collectionDuration prometheus.Gauge
	collectionAttempts prometheus.Counter
	collectionFailures prometheus.Counter
	metricsRequests    prometheus.Counter
	metricsExported    prometheus.Gauge
}

// NewMonitoringMetrics creates a new set of monitoring metrics
func NewMonitoringMetrics() *MonitoringMetrics {
	return &MonitoringMetrics{
		collectionDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "slurm_exporter_collection_duration_seconds",
			Help: "Duration of the most recent metrics collection from SLURM APIs",
		}),
		collectionAttempts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "slurm_exporter_collection_attempts_total",
			Help: "Total number of metrics collection attempts",
		}),
		collectionFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "slurm_exporter_collection_failures_total",
			Help: "Total number of failed metrics collection attempts",
		}),
		metricsRequests: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "slurm_exporter_metrics_requests_total",
			Help: "Total number of requests to /metrics endpoint",
		}),
		metricsExported: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "slurm_exporter_metrics_exported",
			Help: "Number of metrics exported in the last scrape",
		}),
	}
}

// Register registers all monitoring metrics with the given registry
func (m *MonitoringMetrics) Register(registry *prometheus.Registry) error {
	collectors := []prometheus.Collector{
		m.collectionDuration,
		m.collectionAttempts,
		m.collectionFailures,
		m.metricsRequests,
		m.metricsExported,
	}

	for _, collector := range collectors {
		if err := registry.Register(collector); err != nil {
			return err
		}
	}

	return nil
}

// RecordCollection records a collection attempt with its duration and success/failure
func (m *MonitoringMetrics) RecordCollection(duration float64, err error) {
	m.collectionAttempts.Inc()
	m.collectionDuration.Set(duration)
	if err != nil {
		m.collectionFailures.Inc()
	}
}

// RecordMetricsRequest increments the metrics request counter
func (m *MonitoringMetrics) RecordMetricsRequest() {
	m.metricsRequests.Inc()
}

// RecordMetricsExported updates the number of exported metrics
func (m *MonitoringMetrics) RecordMetricsExported(count float64) {
	m.metricsExported.Set(count)
}
