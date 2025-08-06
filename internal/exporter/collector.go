package exporter

import (
	"context"
	"fmt"
	"iter"
	"strconv"
	"sync/atomic"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

// MetricsCollector exposes SLURM metrics by implementing prometheus.Collector interface
type MetricsCollector struct {
	slurmAPIClient slurmapi.Client

	nodeInfo       *prometheus.Desc
	jobInfo        *prometheus.Desc
	jobNode        *prometheus.Desc
	jobDuration    *prometheus.Desc
	nodeGPUSeconds *prometheus.CounterVec
	nodeFails      *prometheus.CounterVec

	rpcCallsTotal               *prometheus.Desc
	rpcDurationSecondsTotal     *prometheus.Desc
	rpcUserCallsTotal           *prometheus.Desc
	rpcUserDurationSecondsTotal *prometheus.Desc
	controllerServerThreadCount *prometheus.Desc

	// Atomic pointer to the current state for lock-free reads
	state atomic.Pointer[metricsCollectorState]
}

// NewMetricsCollector creates a new MetricsCollector
func NewMetricsCollector(slurmAPIClient slurmapi.Client) *MetricsCollector {
	collector := &MetricsCollector{
		slurmAPIClient: slurmAPIClient,

		nodeInfo:    prometheus.NewDesc("slurm_node_info", "Slurm node info", []string{"node_name", "instance_id", "state_base", "state_is_drain", "state_is_maintenance", "state_is_reserved", "address"}, nil),
		jobInfo:     prometheus.NewDesc("slurm_job_info", "Slurm job detail information", []string{"job_id", "job_state", "job_state_reason", "slurm_partition", "job_name", "user_name", "user_id", "standard_error", "standard_output", "array_job_id", "array_task_id", "submit_time", "start_time", "end_time", "finished_time"}, nil),
		jobNode:     prometheus.NewDesc("slurm_node_job", "Slurm job node information", []string{"job_id", "node_name"}, nil),
		jobDuration: prometheus.NewDesc("slurm_job_duration_seconds", "Slurm job duration in seconds", []string{"job_id"}, nil),
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
	}

	collector.state.Store(newMetricsCollectorState())

	return collector
}

func (c *MetricsCollector) updateGPUSecondsMetrics(ctx context.Context, nodes []slurmapi.Node, previousTime time.Time, currentTime time.Time) time.Time {
	logger := log.FromContext(ctx).WithName(ControllerName)

	for _, node := range nodes {
		tres, err := slurmapi.ParseTrackableResources(node.Tres)
		if err != nil {
			logger.Error(err, "Failed to parse trackable resources", "tres", node.Tres)
			continue
		}

		gpuSecondsInc := currentTime.Sub(previousTime).Seconds() * float64(tres.GPUCount)
		c.nodeGPUSeconds.WithLabelValues(
			node.Name,
			string(node.BaseState()),
			strconv.FormatBool(node.IsDrainState()),
			strconv.FormatBool(node.IsMaintenanceState()),
			strconv.FormatBool(node.IsReservedState()),
		).Add(gpuSecondsInc)
	}

	return currentTime
}

func (c *MetricsCollector) updateNodeFailureMetrics(currentNodes []slurmapi.Node, previousNodes []slurmapi.Node) {
	previousNodesMap := make(map[string]slurmapi.Node, len(previousNodes))
	for _, node := range previousNodes {
		previousNodesMap[node.Name] = node
	}

	for _, node := range currentNodes {
		if existingNode, exists := previousNodesMap[node.Name]; exists {
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
	}
}

// Describe implements the prometheus.Collector interface
func (c *MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.nodeInfo
	ch <- c.jobInfo
	ch <- c.jobNode
	ch <- c.jobDuration
	c.nodeGPUSeconds.Describe(ch)
	c.nodeFails.Describe(ch)

	ch <- c.rpcCallsTotal
	ch <- c.rpcDurationSecondsTotal
	ch <- c.rpcUserCallsTotal
	ch <- c.rpcUserDurationSecondsTotal
	ch <- c.controllerServerThreadCount
}

// updateState fetches data from SLURM APIs and atomically updates the collector state
func (c *MetricsCollector) updateState(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName(ControllerName)
	now := time.Now()

	previousState := c.state.Load()
	if previousState == nil {
		previousState = newMetricsCollectorState()
	}

	newState := &metricsCollectorState{
		lastGPUSecondsUpdate: previousState.lastGPUSecondsUpdate,
	}

	// Always update state with whatever data we successfully collect (even if partial)
	defer func() {
		c.state.Store(newState)
	}()

	nodes, err := c.slurmAPIClient.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("get nodes from SLURM API: %w", err)
	}
	newState.nodes = nodes

	c.updateNodeFailureMetrics(nodes, previousState.nodes)
	newState.lastGPUSecondsUpdate = c.updateGPUSecondsMetrics(ctx, nodes, previousState.lastGPUSecondsUpdate, now)

	jobs, err := c.slurmAPIClient.ListJobs(ctx)
	if err != nil {
		return fmt.Errorf("get jobs from SLURM API: %w", err)
	}
	newState.jobs = jobs

	diag, err := c.slurmAPIClient.GetDiag(ctx)
	if err != nil {
		return fmt.Errorf("get diag from SLURM API: %w", err)
	}
	newState.diag = diag

	logger.Info("Collected metrics", "elapsed_seconds", time.Since(now).Seconds())

	return nil
}

// Collect implements the prometheus.Collector interface
func (c *MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()

	state := c.state.Load()
	if state == nil {
		return
	}

	for slurmNodeMetric := range c.slurmNodeMetrics(state.nodes) {
		ch <- slurmNodeMetric
	}

	c.nodeGPUSeconds.Collect(ch)
	c.nodeFails.Collect(ch)

	for slurmJobMetric := range c.slurmJobMetrics(ctx, state.jobs) {
		ch <- slurmJobMetric
	}

	for rpcMetric := range c.slurmRPCMetrics(state.diag) {
		ch <- rpcMetric
	}
}

func (c *MetricsCollector) slurmNodeMetrics(slurmNodes []slurmapi.Node) iter.Seq[prometheus.Metric] {
	return func(yield func(prometheus.Metric) bool) {
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
		}
	}
}

func (c *MetricsCollector) slurmJobMetrics(ctx context.Context, slurmJobs []slurmapi.Job) iter.Seq[prometheus.Metric] {
	return func(yield func(prometheus.Metric) bool) {
		logger := log.FromContext(ctx).WithName(ControllerName)
		for _, job := range slurmJobs {
			userID := ""
			if job.UserID != nil {
				userID = strconv.Itoa(int(*job.UserID))
			}

			var finishedTime string
			if job.IsTerminalState() && job.EndTime != nil && job.EndTime.Unix() != 0 {
				finishedTime = timeToUnixString(job.EndTime)
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
				timeToUnixString(job.SubmitTime),
				timeToUnixString(job.StartTime),
				timeToUnixString(job.EndTime),
				finishedTime,
			}
			yield(prometheus.MustNewConstMetric(c.jobInfo, prometheus.GaugeValue, 1, jobLabels...))

			// Calculate job duration
			if job.StartTime != nil && job.StartTime.Unix() != 0 {
				var endTime time.Time
				if job.IsTerminalState() && job.EndTime != nil && job.EndTime.Unix() != 0 {
					endTime = job.EndTime.Time
				} else if !job.IsTerminalState() {
					endTime = time.Now()
				}

				if !endTime.IsZero() {
					duration := endTime.Sub(job.StartTime.Time).Seconds()
					if duration > 0 {
						yield(prometheus.MustNewConstMetric(c.jobDuration, prometheus.GaugeValue, duration, job.GetIDString()))
					}
				}
			}

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

func (c *MetricsCollector) slurmRPCMetrics(diag *api.V0041OpenapiDiagResp) iter.Seq[prometheus.Metric] {
	return func(yield func(prometheus.Metric) bool) {
		if diag == nil {
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

func timeToUnixString(t *metav1.Time) string {
	if t == nil || t.Unix() == 0 {
		return ""
	}
	return strconv.FormatInt(t.Unix(), 10)
}
