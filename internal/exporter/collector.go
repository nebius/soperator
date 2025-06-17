package exporter

import (
	"context"
	"iter"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

// MetricsCollector exposes SLURM metrics by implementing prometheus.Collector interface
type MetricsCollector struct {
	slurmAPIClient slurmapi.Client

	sopClusterInfo *prometheus.Desc
	nodeInfo       *prometheus.Desc
	jobInfo        *prometheus.Desc
	jobNode        *prometheus.Desc
	nodeGPUSeconds *prometheus.CounterVec
	jobGPUSeconds  *prometheus.CounterVec
	nodeFails      *prometheus.CounterVec

	lastNodeGPUTimeUpdated time.Time
	lastJobGPUTimeUpdated  time.Time
	nodes                  map[string]slurmapi.Node
	stateMutex             sync.RWMutex
}

// NewMetricsCollector creates a new MetricsCollector
func NewMetricsCollector(slurmAPIClient slurmapi.Client, soperatorVersion string) *MetricsCollector {
	sopClusterInfoConstLabels := prometheus.Labels{"soperator_version": soperatorVersion}
	return &MetricsCollector{
		slurmAPIClient: slurmAPIClient,

		sopClusterInfo: prometheus.NewDesc("soperator_cluster_info", "Soperator cluster information", []string{}, sopClusterInfoConstLabels),
		nodeInfo:       prometheus.NewDesc("slurm_node_info", "Slurm node info", []string{"node_name", "compute_instance_id", "base_state", "is_drain", "address"}, nil),
		jobInfo:        prometheus.NewDesc("slurm_job_info", "Slurm job detail information", []string{"job_id", "job_state", "job_state_reason", "slurm_partition", "job_name", "user_name", "standard_error", "standard_output", "array_job_id", "array_task_id"}, nil),
		jobNode:        prometheus.NewDesc("slurm_node_job", "Slurm job node information", []string{"job_id", "node_name"}, nil),
		nodeGPUSeconds: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "slurm_active_node_gpu_seconds_total",
			Help: "Total GPU seconds on active Slurm nodes (not down, not idle+drain)",
		}, []string{"node_name"}),
		jobGPUSeconds: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "slurm_job_alloc_gpu_seconds_total",
			Help: "Total GPU seconds allocated to jobs in RUNNING state",
		}, []string{}),
		nodeFails: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "slurm_node_fails_total",
			Help: "Total number of times a node has failed (went from not down/drain to down/drain state)",
		}, []string{"node_name", "reason"}),

		lastNodeGPUTimeUpdated: time.Now(),
		lastJobGPUTimeUpdated:  time.Now(),
		nodes:                  make(map[string]slurmapi.Node),
	}
}

// Describe implements the prometheus.Collector interface
func (c *MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.sopClusterInfo
	ch <- c.nodeInfo
	ch <- c.jobInfo
	ch <- c.jobNode
	c.nodeGPUSeconds.Describe(ch)
	c.jobGPUSeconds.Describe(ch)
	c.nodeFails.Describe(ch)
}

// Collect implements the prometheus.Collector interface
func (c *MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	c.stateMutex.Lock()
	defer c.stateMutex.Unlock()

	ctx := context.Background()
	now := time.Now()
	ch <- prometheus.MustNewConstMetric(c.sopClusterInfo, prometheus.GaugeValue, 1)
	logger := log.FromContext(ctx).WithName(ControllerName)
	nodes, err := c.slurmAPIClient.ListNodes(ctx)
	if err != nil {
		logger.Error(err, "Failed to get nodes from SLURM API")
		return
	}
	for slurmNodeMetric := range c.slurmNodeMetrics(ctx, now, nodes) {
		ch <- slurmNodeMetric
	}

	for _, node := range nodes {
		if existingNode, exists := c.nodes[node.Name]; exists {
			wasFailed := existingNode.IsDownState() || existingNode.IsDrainState()
			isFailed := node.IsDownState() || node.IsDrainState()
			if !wasFailed && isFailed {
				var reason string
				if node.Reason != nil {
					reason = node.Reason.Reason
				}
				c.nodeFails.WithLabelValues(node.Name, reason).Inc()
			}
		}
		c.nodes[node.Name] = node
	}

	c.nodeGPUSeconds.Collect(ch)
	c.nodeFails.Collect(ch)

	jobs, err := c.slurmAPIClient.ListJobs(ctx)
	if err != nil {
		logger.Error(err, "Failed to get jobs from SLURM API")
		return
	}

	c.calculateJobGPUSeconds(ctx, now, jobs, nodes)
	c.jobGPUSeconds.Collect(ch)

	for slurmJobMetric := range c.slurmJobMetrics(ctx, jobs) {
		ch <- slurmJobMetric
	}

	logger.Info("Collected metrics", "elapsed_seconds", time.Now().Sub(now).Seconds())
}

func (c *MetricsCollector) slurmNodeMetrics(
	ctx context.Context, now time.Time, slurmNodes []slurmapi.Node,
) iter.Seq[prometheus.Metric] {
	return func(yield func(prometheus.Metric) bool) {
		logger := log.FromContext(ctx).WithName(ControllerName)
		for _, node := range slurmNodes {
			labels := []string{
				node.Name,
				node.InstanceID,
				string(node.BaseState()),
				strconv.FormatBool(node.IsDrainState()),
				node.Address,
			}
			yield(prometheus.MustNewConstMetric(c.nodeInfo, prometheus.GaugeValue, 1, labels...))

			tres, err := slurmapi.ParseTrackableResources(node.Tres)
			if err != nil {
				logger.Error(err, "Failed to parse trackable resources", "tres", node.Tres)
				continue
			}
			if !node.IsDownState() && !node.IsIdleDrained() {
				gpuSecondsInc := now.Sub(c.lastNodeGPUTimeUpdated).Seconds() * float64(tres.GPUCount)
				c.nodeGPUSeconds.WithLabelValues(node.Name).Add(gpuSecondsInc)
			}
		}
		c.lastNodeGPUTimeUpdated = now
	}
}

func (c *MetricsCollector) calculateJobGPUSeconds(
	ctx context.Context, now time.Time, jobs []slurmapi.Job, nodes []slurmapi.Node,
) {
	logger := log.FromContext(ctx).WithName(ControllerName)

	nodeMap := make(map[string]slurmapi.Node, len(nodes))
	for _, node := range nodes {
		nodeMap[node.Name] = node
	}

	var totalRunningJobGPUs int
	for _, job := range jobs {
		if job.State != "RUNNING" {
			continue
		}

		nodeList, err := job.GetNodeList()
		if err != nil {
			logger.Error(err, "Failed to parse node list for job", "job_id", job.GetIDString(), "nodes", job.Nodes)
			continue
		}

		for _, nodeName := range nodeList {
			node, exists := nodeMap[nodeName]
			if !exists {
				logger.Error(nil, "Job references unknown node", "job_id", job.GetIDString(), "node_name", nodeName)
				continue
			}

			tres, err := slurmapi.ParseTrackableResources(node.Tres)
			if err != nil {
				logger.Error(err, "Failed to parse trackable resources for job node", "job_id", job.GetIDString(), "node_name", nodeName, "tres", node.Tres)
				continue
			}

			totalRunningJobGPUs += tres.GPUCount
		}
	}

	if totalRunningJobGPUs > 0 {
		timeDelta := now.Sub(c.lastJobGPUTimeUpdated).Seconds()
		gpuSecondsInc := timeDelta * float64(totalRunningJobGPUs)
		c.jobGPUSeconds.WithLabelValues().Add(gpuSecondsInc)
	}

	c.lastJobGPUTimeUpdated = now
}

func (c *MetricsCollector) slurmJobMetrics(
	ctx context.Context, slurmJobs []slurmapi.Job,
) iter.Seq[prometheus.Metric] {
	return func(yield func(prometheus.Metric) bool) {
		logger := log.FromContext(ctx).WithName(ControllerName)
		for _, job := range slurmJobs {
			jobLabels := []string{
				job.GetIDString(),
				job.State,
				job.StateReason,
				job.Partition,
				job.Name,
				job.UserName,
				job.StandardError,
				job.StandardOutput,
				job.GetArrayJobIDString(),
				job.GetArrayTaskIDString(),
			}
			yield(prometheus.MustNewConstMetric(c.jobInfo, prometheus.GaugeValue, 1, jobLabels...))

			nodeList, err := job.GetNodeList()
			if err != nil {
				logger.Error(err, "Failed to parse node list for job", "job_id", job.GetIDString(), "nodes", job.Nodes)
				continue
			}
			for _, nodeName := range nodeList {
				jobNodeLabels := []string{job.GetIDString(), nodeName}
				yield(prometheus.MustNewConstMetric(c.jobNode, prometheus.GaugeValue, 1, jobNodeLabels...))
			}
		}
	}
}
