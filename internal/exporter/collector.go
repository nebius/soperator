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
	userResolver   *UserResolver

	nodeInfo       *prometheus.Desc
	jobInfo        *prometheus.Desc
	jobNode        *prometheus.Desc
	nodeGPUSeconds *prometheus.CounterVec
	nodeFails      *prometheus.CounterVec

	rpcCallsTotal               *prometheus.Desc
	rpcDurationSecondsTotal     *prometheus.Desc
	rpcUserCallsTotal           *prometheus.Desc
	rpcUserDurationSecondsTotal *prometheus.Desc
	controllerServerThreadCount *prometheus.Desc

	lastNodeGPUTimeUpdated time.Time
	nodes                  map[string]slurmapi.Node
	stateMutex             sync.RWMutex
}

// NewMetricsCollector creates a new MetricsCollector
func NewMetricsCollector(slurmAPIClient slurmapi.Client) *MetricsCollector {
	return &MetricsCollector{
		slurmAPIClient: slurmAPIClient,
		userResolver:   NewUserResolver(),

		nodeInfo: prometheus.NewDesc("slurm_node_info", "Slurm node info", []string{"node_name", "instance_id", "state_base", "state_is_drain", "state_is_maintenance", "state_is_reserved", "address"}, nil),
		jobInfo:  prometheus.NewDesc("slurm_job_info", "Slurm job detail information", []string{"job_id", "job_state", "job_state_reason", "slurm_partition", "job_name", "user_name", "user_id", "standard_error", "standard_output", "array_job_id", "array_task_id"}, nil),
		jobNode:  prometheus.NewDesc("slurm_node_job", "Slurm job node information", []string{"job_id", "node_name"}, nil),
		nodeGPUSeconds: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "slurm_node_gpu_seconds_total",
			Help: "Total GPU seconds on Slurm nodes",
		}, []string{"node_name", "state_base", "state_is_drain", "state_is_maintenance", "state_is_reserved"}),
		nodeFails: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "slurm_node_fails_total",
			Help: "Total number of times a node has failed (went from not down/drain to down/drain state)",
		}, []string{"node_name", "state_base", "state_is_drain", "state_is_maintenance", "state_is_reserved", "reason"}),

		rpcCallsTotal:               prometheus.NewDesc("slurm_controller_rpc_calls_total", "Total count of RPC calls by message type", []string{"message_type"}, nil),
		rpcDurationSecondsTotal:     prometheus.NewDesc("slurm_controller_rpc_duration_seconds_total", "Total time spent processing RPCs by message type", []string{"message_type"}, nil),
		rpcUserCallsTotal:           prometheus.NewDesc("slurm_controller_rpc_user_calls_total", "Total count of RPC calls by user", []string{"user", "user_id"}, nil),
		rpcUserDurationSecondsTotal: prometheus.NewDesc("slurm_controller_rpc_user_duration_seconds_total", "Total time spent on user RPCs", []string{"user", "user_id"}, nil),
		controllerServerThreadCount: prometheus.NewDesc("slurm_controller_server_thread_count", "Number of server threads", nil, nil),

		lastNodeGPUTimeUpdated: time.Now(),
		nodes:                  make(map[string]slurmapi.Node),
	}
}

// Describe implements the prometheus.Collector interface
func (c *MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.nodeInfo
	ch <- c.jobInfo
	ch <- c.jobNode
	c.nodeGPUSeconds.Describe(ch)
	c.nodeFails.Describe(ch)

	ch <- c.rpcCallsTotal
	ch <- c.rpcDurationSecondsTotal
	ch <- c.rpcUserCallsTotal
	ch <- c.rpcUserDurationSecondsTotal
	ch <- c.controllerServerThreadCount
}

// Collect implements the prometheus.Collector interface
func (c *MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	c.stateMutex.Lock()
	defer c.stateMutex.Unlock()

	ctx := context.Background()
	now := time.Now()
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
				c.nodeFails.WithLabelValues(
					node.Name,
					string(node.BaseState()),
					strconv.FormatBool(node.IsDrainState()),
					strconv.FormatBool(node.IsMaintenanceState()),
					strconv.FormatBool(node.IsReservedState()),
					reason,
				).Inc()
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

	resolvedJobs := c.resolveJobUsernames(ctx, jobs)

	for slurmJobMetric := range c.slurmJobMetrics(ctx, resolvedJobs) {
		ch <- slurmJobMetric
	}

	for rpcMetric := range c.slurmRPCMetrics(ctx) {
		ch <- rpcMetric
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
				strconv.FormatBool(node.IsMaintenanceState()),
				strconv.FormatBool(node.IsReservedState()),
				node.Address,
			}
			yield(prometheus.MustNewConstMetric(c.nodeInfo, prometheus.GaugeValue, 1, labels...))

			tres, err := slurmapi.ParseTrackableResources(node.Tres)
			if err != nil {
				logger.Error(err, "Failed to parse trackable resources", "tres", node.Tres)
				continue
			}
			gpuSecondsInc := now.Sub(c.lastNodeGPUTimeUpdated).Seconds() * float64(tres.GPUCount)
			c.nodeGPUSeconds.WithLabelValues(
				node.Name,
				string(node.BaseState()),
				strconv.FormatBool(node.IsDrainState()),
				strconv.FormatBool(node.IsMaintenanceState()),
				strconv.FormatBool(node.IsReservedState()),
			).Add(gpuSecondsInc)
		}
		c.lastNodeGPUTimeUpdated = now
	}
}

func (c *MetricsCollector) resolveJobUsernames(ctx context.Context, jobs []slurmapi.Job) []slurmapi.Job {
	logger := log.FromContext(ctx).WithName(ControllerName)

	userMap, err := c.userResolver.GetUserMap()
	if err != nil {
		logger.Error(err, "Failed to get user map from passwd file")
		return jobs // Return jobs as-is if we can't get user map
	}

	resolvedJobs := make([]slurmapi.Job, 0, len(jobs))
	for _, job := range jobs {
		resolvedJob := job

		if job.UserName == "" && job.UserID != nil {
			if username, exists := userMap[*job.UserID]; exists {
				resolvedJob.UserName = username
			} else {
				logger.Error(nil, "User ID not found in passwd file", "job_id", job.GetIDString(), "user_id", *job.UserID)
			}
		}

		resolvedJobs = append(resolvedJobs, resolvedJob)
	}

	return resolvedJobs
}

func (c *MetricsCollector) slurmJobMetrics(
	ctx context.Context, slurmJobs []slurmapi.Job,
) iter.Seq[prometheus.Metric] {
	return func(yield func(prometheus.Metric) bool) {
		logger := log.FromContext(ctx).WithName(ControllerName)
		for _, job := range slurmJobs {
			userID := ""
			if job.UserID != nil {
				userID = strconv.Itoa(int(*job.UserID))
			}

			jobLabels := []string{
				job.GetIDString(),
				job.State,
				job.StateReason,
				job.Partition,
				job.Name,
				job.UserName,
				userID,
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

func (c *MetricsCollector) slurmRPCMetrics(
	ctx context.Context,
) iter.Seq[prometheus.Metric] {
	return func(yield func(prometheus.Metric) bool) {
		logger := log.FromContext(ctx).WithName(ControllerName)

		diag, err := c.slurmAPIClient.GetDiag(ctx)
		if err != nil {
			logger.Error(err, "Failed to get diagnostics from SLURM API")
			return
		}

		stats := diag.Statistics

		if stats.ServerThreadCount != nil {
			yield(prometheus.MustNewConstMetric(c.controllerServerThreadCount, prometheus.GaugeValue, float64(*stats.ServerThreadCount)))
		}
		if stats.RpcsByMessageType != nil {
			for _, rpc := range *stats.RpcsByMessageType {
				messageType := rpc.MessageType

				if rpc.Count > 0 {
					yield(prometheus.MustNewConstMetric(c.rpcCallsTotal, prometheus.CounterValue, float64(rpc.Count), messageType))
				}

				if rpc.TotalTime > 0 {
					durationSeconds := float64(rpc.TotalTime) / 1_000_000
					yield(prometheus.MustNewConstMetric(c.rpcDurationSecondsTotal, prometheus.CounterValue, durationSeconds, messageType))
				}
			}
		}

		if stats.RpcsByUser != nil {
			for _, userRpc := range *stats.RpcsByUser {
				user := userRpc.User
				userID := strconv.Itoa(int(userRpc.UserId))

				if userRpc.Count > 0 {
					yield(prometheus.MustNewConstMetric(c.rpcUserCallsTotal, prometheus.CounterValue, float64(userRpc.Count), user, userID))
				}

				if userRpc.TotalTime > 0 {
					durationSeconds := float64(userRpc.TotalTime) / 1_000_000
					yield(prometheus.MustNewConstMetric(c.rpcUserDurationSecondsTotal, prometheus.CounterValue, durationSeconds, user, userID))
				}
			}
		}
	}
}
