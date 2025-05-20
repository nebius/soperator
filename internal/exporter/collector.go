package exporter

import (
	"github.com/prometheus/client_golang/prometheus"
)

// MetricsCollector exposes SLURM metrics by implementing prometheus.Collector interface
type MetricsCollector struct {
	clusterInfo *prometheus.Desc
	nodeInfo    *prometheus.Desc
	jobInfo     *prometheus.Desc
	jobNode     *prometheus.Desc
}

// NewMetricsCollector creates a new MetricsCollector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		clusterInfo: prometheus.NewDesc("slurm_cluster_info", "Slurm cluster information", []string{"testlabel"}, nil),
		nodeInfo:    prometheus.NewDesc("slurm_node_info", "Slurm node detail information", []string{}, nil),
		jobInfo:     prometheus.NewDesc("slurm_job_info", "Slurm job detail information", []string{}, nil),
		jobNode:     prometheus.NewDesc("slurm_node_job", "Slurm job node information", []string{"job_id", "node_name"}, nil),
	}
}

// Describe implements the prometheus.Collector interface
func (c *MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.clusterInfo
	ch <- c.nodeInfo
	ch <- c.jobInfo
	ch <- c.jobNode
}

// Collect implements the prometheus.Collector interface
func (c *MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(c.clusterInfo, prometheus.GaugeValue, 1, "test value")
	// TODO: expose other metrics.
}
