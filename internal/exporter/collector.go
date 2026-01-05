package exporter

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"strconv"
	"sync/atomic"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

// MetricsCollector exposes SLURM metrics by implementing prometheus.Collector interface
type MetricsCollector struct {
	slurmAPIClient slurmapi.Client

	nodeInfo                   *prometheus.Desc
	nodeCPUTotal               *prometheus.Desc
	nodeCPUAllocated           *prometheus.Desc
	nodeCPUIdle                *prometheus.Desc
	nodeCPUEffective           *prometheus.Desc
	nodeMemoryTotalBytes       *prometheus.Desc
	nodeMemoryAllocatedBytes   *prometheus.Desc
	nodeMemoryFreeBytes        *prometheus.Desc
	nodeMemoryEffectiveBytes   *prometheus.Desc
	nodePartition              *prometheus.Desc
	jobInfo                    *prometheus.Desc
	jobNode                    *prometheus.Desc
	jobDuration                *prometheus.Desc
	jobCPUs                    *prometheus.Desc
	jobMemoryBytes             *prometheus.Desc
	nodeGPUSeconds             *prometheus.CounterVec
	nodeFails                  *prometheus.CounterVec
	nodeUnavailabilityDuration *prometheus.HistogramVec
	nodeDrainingDuration       *prometheus.HistogramVec

	rpcCallsTotal               *prometheus.Desc
	rpcDurationSecondsTotal     *prometheus.Desc
	rpcUserCallsTotal           *prometheus.Desc
	rpcUserDurationSecondsTotal *prometheus.Desc
	controllerServerThreadCount *prometheus.Desc

	// Atomic pointer to the current state for lock-free reads
	state atomic.Pointer[metricsCollectorState]

	// Monitoring contains self-monitoring metrics
	Monitoring *MonitoringMetrics
}

// durationBuckets defines histogram buckets for duration metrics ranging from 30 seconds to 30 days
var durationBuckets = []float64{
	30 * time.Second.Seconds(),
	1 * time.Minute.Seconds(),
	5 * time.Minute.Seconds(),
	10 * time.Minute.Seconds(),
	30 * time.Minute.Seconds(),
	1 * time.Hour.Seconds(),
	2 * time.Hour.Seconds(),
	4 * time.Hour.Seconds(),
	8 * time.Hour.Seconds(),
	12 * time.Hour.Seconds(),
	24 * time.Hour.Seconds(),
	2 * 24 * time.Hour.Seconds(),
	3 * 24 * time.Hour.Seconds(),
	7 * 24 * time.Hour.Seconds(),
	14 * 24 * time.Hour.Seconds(),
	30 * 24 * time.Hour.Seconds(),
}

// NewMetricsCollector creates a new MetricsCollector
func NewMetricsCollector(slurmAPIClient slurmapi.Client) *MetricsCollector {
	collector := &MetricsCollector{
		slurmAPIClient: slurmAPIClient,
		Monitoring:     NewMonitoringMetrics(),

		nodeInfo:                 prometheus.NewDesc("slurm_node_info", "Slurm node info", []string{"node_name", "instance_id", "state_base", "state_is_drain", "state_is_maintenance", "state_is_reserved", "state_is_completing", "state_is_fail", "state_is_planned", "reservation_name", "address", "reason"}, nil),
		nodeCPUTotal:             prometheus.NewDesc("slurm_node_cpus_total", "Total CPUs on the node", []string{"node_name"}, nil),
		nodeCPUAllocated:         prometheus.NewDesc("slurm_node_cpus_allocated", "CPUs allocated on the node", []string{"node_name"}, nil),
		nodeCPUIdle:              prometheus.NewDesc("slurm_node_cpus_idle", "Idle CPUs on the node", []string{"node_name"}, nil),
		nodeCPUEffective:         prometheus.NewDesc("slurm_node_cpus_effective", "Effective CPUs on the node", []string{"node_name"}, nil),
		nodeMemoryTotalBytes:     prometheus.NewDesc("slurm_node_memory_total_bytes", "Total memory on the node in bytes", []string{"node_name"}, nil),
		nodeMemoryAllocatedBytes: prometheus.NewDesc("slurm_node_memory_allocated_bytes", "Allocated memory on the node in bytes", []string{"node_name"}, nil),
		nodeMemoryFreeBytes:      prometheus.NewDesc("slurm_node_memory_free_bytes", "Free memory on the node in bytes", []string{"node_name"}, nil),
		nodeMemoryEffectiveBytes: prometheus.NewDesc("slurm_node_memory_effective_bytes", "Effective memory on the node in bytes", []string{"node_name"}, nil),
		nodePartition:            prometheus.NewDesc("slurm_node_partition", "Slurm node partition mapping", []string{"node_name", "partition"}, nil),
		jobInfo:                  prometheus.NewDesc("slurm_job_info", "Slurm job detail information", []string{"job_id", "job_state", "job_state_reason", "slurm_partition", "job_name", "user_name", "user_mail", "user_id", "standard_error", "standard_output", "array_job_id", "array_task_id", "submit_time", "start_time", "end_time", "finished_time"}, nil),
		jobNode:                  prometheus.NewDesc("slurm_node_job", "Slurm job node information", []string{"job_id", "node_name"}, nil),
		jobDuration:              prometheus.NewDesc("slurm_job_duration_seconds", "Slurm job duration in seconds", []string{"job_id"}, nil),
		jobCPUs:                  prometheus.NewDesc("slurm_job_cpus", "CPUs allocated to a Slurm job", []string{"job_id"}, nil),
		jobMemoryBytes:           prometheus.NewDesc("slurm_job_memory_bytes", "Memory allocated to a Slurm job in bytes", []string{"job_id"}, nil),
		nodeGPUSeconds: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "slurm_node_gpu_seconds_total",
			Help: "Total GPU seconds on Slurm nodes",
		}, []string{"node_name", "state_base", "state_is_drain", "state_is_maintenance", "state_is_reserved"}),
		nodeFails: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "slurm_node_fails_total",
			Help: "Total number of times a node has failed (went from not down/drain to down/drain state)",
		}, []string{"node_name", "state_base", "state_is_drain", "state_is_maintenance", "state_is_reserved", "reason"}),
		nodeUnavailabilityDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "slurm_node_unavailability_duration_seconds",
			Help:    "Duration of completed node unavailability events (DOWN+* or IDLE+DRAIN+*)",
			Buckets: durationBuckets,
		}, []string{"node_name"}),
		nodeDrainingDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "slurm_node_draining_duration_seconds",
			Help:    "Duration of completed node draining events (DRAIN+ALLOCATED or DRAIN+MIXED)",
			Buckets: durationBuckets,
		}, []string{"node_name"}),

		rpcCallsTotal:               prometheus.NewDesc("slurm_controller_rpc_calls_total", "Total count of RPC calls by message type", []string{"message_type"}, nil),
		rpcDurationSecondsTotal:     prometheus.NewDesc("slurm_controller_rpc_duration_seconds_total", "Total time spent processing RPCs by message type", []string{"message_type"}, nil),
		rpcUserCallsTotal:           prometheus.NewDesc("slurm_controller_rpc_user_calls_total", "Total count of RPC calls by user", []string{"user", "user_id"}, nil),
		rpcUserDurationSecondsTotal: prometheus.NewDesc("slurm_controller_rpc_user_duration_seconds_total", "Total time spent on user RPCs", []string{"user", "user_id"}, nil),
		controllerServerThreadCount: prometheus.NewDesc("slurm_controller_server_thread_count", "Number of server threads", nil, nil),
	}

	collector.state.Store(newMetricsCollectorState())

	return collector
}

// isNodeUnavailable checks if a node is in unavailable state
// Unavailable state: DOWN+* or IDLE+DRAIN+*
func isNodeUnavailable(node slurmapi.Node) bool {
	if node.IsDownState() {
		return true
	}
	if node.BaseState() == api.V0041NodeStateIDLE && node.IsDrainState() {
		return true
	}
	return false
}

// isNodeDraining checks if a node is in draining state
// Draining state: DRAIN+ALLOC+* or DRAIN+MIXED+*
func isNodeDraining(node slurmapi.Node) bool {
	if !node.IsDrainState() {
		return false
	}
	baseState := node.BaseState()
	return baseState == api.V0041NodeStateALLOCATED || baseState == api.V0041NodeStateMIXED
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

func (c *MetricsCollector) updateNodeStateMetrics(currentNodes []slurmapi.Node, previousState *metricsCollectorState, newState *metricsCollectorState, currentTime time.Time) {
	previousNodesMap := make(map[string]slurmapi.Node)
	if previousState != nil {
		for _, node := range previousState.nodes {
			previousNodesMap[node.Name] = node
		}
	}

	for _, node := range currentNodes {
		previousNode, existed := previousNodesMap[node.Name]

		// Nested helper to process state transitions
		processTransition := func(stateChecker func(slurmapi.Node) bool, startTimes map[string]time.Time, histogram *prometheus.HistogramVec) {
			current := stateChecker(node)
			previous := existed && stateChecker(previousNode)

			if current && !previous {
				startTimes[node.Name] = currentTime
			} else if !current && previous {
				if startTime, ok := startTimes[node.Name]; ok {
					duration := currentTime.Sub(startTime).Seconds()
					histogram.WithLabelValues(node.Name).Observe(duration)
					delete(startTimes, node.Name)
				}
			}
		}

		processTransition(isNodeUnavailable, newState.nodeUnavailabilityStartTimes, c.nodeUnavailabilityDuration)
		processTransition(isNodeDraining, newState.nodeDrainingStartTimes, c.nodeDrainingDuration)
	}
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
	ch <- c.nodeCPUTotal
	ch <- c.nodeCPUAllocated
	ch <- c.nodeCPUIdle
	ch <- c.nodeCPUEffective
	ch <- c.nodeMemoryTotalBytes
	ch <- c.nodeMemoryAllocatedBytes
	ch <- c.nodeMemoryFreeBytes
	ch <- c.nodeMemoryEffectiveBytes
	ch <- c.nodePartition
	ch <- c.jobInfo
	ch <- c.jobNode
	ch <- c.jobDuration
	ch <- c.jobCPUs
	ch <- c.jobMemoryBytes
	c.nodeGPUSeconds.Describe(ch)
	c.nodeFails.Describe(ch)
	c.nodeUnavailabilityDuration.Describe(ch)
	c.nodeDrainingDuration.Describe(ch)

	ch <- c.rpcCallsTotal
	ch <- c.rpcDurationSecondsTotal
	ch <- c.rpcUserCallsTotal
	ch <- c.rpcUserDurationSecondsTotal
	ch <- c.controllerServerThreadCount
}

// updateState fetches data from SLURM APIs and atomically updates the collector state
func (c *MetricsCollector) updateState(ctx context.Context) (err error) {
	logger := log.FromContext(ctx).WithName(ControllerName)
	startTime := time.Now()

	defer func() {
		duration := time.Since(startTime).Seconds()
		c.Monitoring.RecordCollection(duration, err)
	}()

	previousState := c.state.Load()
	if previousState == nil {
		previousState = newMetricsCollectorState()
	}

	newState := newMetricsCollectorState()
	// Copy timestamps in case we fail to get nodes/jobs, so they will be preserved in the new state.
	newState.lastGPUSecondsUpdate = previousState.lastGPUSecondsUpdate
	maps.Copy(newState.nodeUnavailabilityStartTimes, previousState.nodeUnavailabilityStartTimes)
	maps.Copy(newState.nodeDrainingStartTimes, previousState.nodeDrainingStartTimes)

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
	c.updateNodeStateMetrics(nodes, previousState, newState, time.Now())
	newState.lastGPUSecondsUpdate = c.updateGPUSecondsMetrics(ctx, nodes, previousState.lastGPUSecondsUpdate, time.Now())

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

	logger.Info("Collected metrics", "elapsed_seconds", time.Since(startTime).Seconds())

	return nil
}

// Collect implements the prometheus.Collector interface
func (c *MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	metricsChan := make(chan prometheus.Metric)
	go func() {
		c.collectImpl(metricsChan)
		close(metricsChan)
	}()

	var metricsCount float64
	for metric := range metricsChan {
		ch <- metric
		metricsCount++
	}

	// Record the number of metrics exported
	c.Monitoring.RecordMetricsExported(metricsCount)
}

// collectImpl performs the actual metrics collection
func (c *MetricsCollector) collectImpl(ch chan<- prometheus.Metric) {
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
	c.nodeUnavailabilityDuration.Collect(ch)
	c.nodeDrainingDuration.Collect(ch)

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
			var reason string
			if node.Reason != nil {
				reason = node.Reason.Reason
			}
			labels := []string{
				node.Name,
				node.InstanceID,
				string(node.BaseState()),
				strconv.FormatBool(node.IsDrainState()),       // Keep "true"/"false" for backward compatibility
				strconv.FormatBool(node.IsMaintenanceState()), // Keep "true"/"false" for backward compatibility
				strconv.FormatBool(node.IsReservedState()),    // Keep "true"/"false" for backward compatibility
				boolToLabelValue(node.IsCompletingState()),    // New flag: use empty string instead of "false"
				boolToLabelValue(node.IsFailState()),          // New flag: use empty string instead of "false"
				boolToLabelValue(node.IsPlannedState()),       // New flag: use empty string instead of "false"
				trimReservationName(node.Reservation),
				node.Address,
				reason,
			}
			if !yield(prometheus.MustNewConstMetric(c.nodeInfo, prometheus.GaugeValue, 1, labels...)) {
				return
			}

			if cpuTotal, ok := node.CPUTotal(); ok {
				if !yield(prometheus.MustNewConstMetric(c.nodeCPUTotal, prometheus.GaugeValue, cpuTotal, node.Name)) {
					return
				}
			}
			if cpuAlloc, ok := node.CPUAllocated(); ok {
				if !yield(prometheus.MustNewConstMetric(c.nodeCPUAllocated, prometheus.GaugeValue, cpuAlloc, node.Name)) {
					return
				}
			}
			if cpuIdle, ok := node.CPUIdle(); ok {
				if !yield(prometheus.MustNewConstMetric(c.nodeCPUIdle, prometheus.GaugeValue, cpuIdle, node.Name)) {
					return
				}
			}
			if cpuEffective, ok := node.CPUEffective(); ok {
				if !yield(prometheus.MustNewConstMetric(c.nodeCPUEffective, prometheus.GaugeValue, cpuEffective, node.Name)) {
					return
				}
			}

			if memTotal, ok := node.MemoryTotalBytes(); ok {
				if !yield(prometheus.MustNewConstMetric(c.nodeMemoryTotalBytes, prometheus.GaugeValue, memTotal, node.Name)) {
					return
				}
			}
			if memAlloc, ok := node.MemoryAllocatedBytes(); ok {
				if !yield(prometheus.MustNewConstMetric(c.nodeMemoryAllocatedBytes, prometheus.GaugeValue, memAlloc, node.Name)) {
					return
				}
			}
			if memFree, ok := node.MemoryFreeBytes(); ok {
				if !yield(prometheus.MustNewConstMetric(c.nodeMemoryFreeBytes, prometheus.GaugeValue, memFree, node.Name)) {
					return
				}
			}
			if memEff, ok := node.MemoryEffectiveBytes(); ok {
				if !yield(prometheus.MustNewConstMetric(c.nodeMemoryEffectiveBytes, prometheus.GaugeValue, memEff, node.Name)) {
					return
				}
			}

			for _, partition := range node.Partitions {
				if !yield(prometheus.MustNewConstMetric(c.nodePartition, prometheus.GaugeValue, 1, node.Name, partition)) {
					return
				}
			}
		}
	}
}

func mbToBytes(mb int64) float64 {
	return float64(mb) * 1024 * 1024
}

// boolToLabelValue converts a boolean to a label value.
// Returns "true" if true, empty string if false.
// This is used for new state flags to avoid hitting Victoria Metrics' 30 label limit
// (empty label value === no label).
func boolToLabelValue(b bool) string {
	if b {
		return "true"
	}
	return ""
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
				job.UserMail,
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
			if !yield(prometheus.MustNewConstMetric(c.jobInfo, prometheus.GaugeValue, 1, jobLabels...)) {
				return
			}

			cpuValue, cpuOK, memValue, memOK := jobAllocatedResources(logger, job)
			if cpuOK {
				if !yield(prometheus.MustNewConstMetric(c.jobCPUs, prometheus.GaugeValue, cpuValue, job.GetIDString())) {
					return
				}
			}
			if memOK {
				if !yield(prometheus.MustNewConstMetric(c.jobMemoryBytes, prometheus.GaugeValue, memValue, job.GetIDString())) {
					return
				}
			}

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
						if !yield(prometheus.MustNewConstMetric(c.jobDuration, prometheus.GaugeValue, duration, job.GetIDString())) {
							return
						}
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
				if !yield(prometheus.MustNewConstMetric(c.jobNode, prometheus.GaugeValue, 1, jobNodeLabels...)) {
					return
				}
			}
		}
	}
}

func jobAllocatedResources(logger logr.Logger, job slurmapi.Job) (cpu float64, cpuOK bool, mem float64, memOK bool) {
	if job.TresAllocated != "" {
		tres, err := slurmapi.ParseTrackableResources(job.TresAllocated)
		if err != nil {
			logger.Error(err, "Failed to parse job allocated resources", "job_id", job.GetIDString(), "tres", job.TresAllocated)
		} else {
			if tres.CPUCount > 0 {
				cpu = float64(tres.CPUCount)
				cpuOK = true
			}
			if tres.MemoryBytes > 0 {
				mem = float64(tres.MemoryBytes)
				memOK = true
			}
		}
	}

	if !cpuOK && job.CPUs != nil {
		cpu = float64(*job.CPUs)
		cpuOK = true
	}

	if !memOK && job.MemoryPerNode != nil {
		nodeCount := int32(1)
		if job.NodeCount != nil {
			nodeCount = *job.NodeCount
		}
		mem = mbToBytes(*job.MemoryPerNode * int64(nodeCount))
		memOK = true
	}

	return cpu, cpuOK, mem, memOK
}

func (c *MetricsCollector) slurmRPCMetrics(diag *api.V0041OpenapiDiagResp) iter.Seq[prometheus.Metric] {
	return func(yield func(prometheus.Metric) bool) {
		if diag == nil {
			return
		}

		stats := diag.Statistics

		if stats.ServerThreadCount != nil {
			if !yield(prometheus.MustNewConstMetric(c.controllerServerThreadCount, prometheus.GaugeValue, float64(*stats.ServerThreadCount))) {
				return
			}
		}
		if stats.RpcsByMessageType != nil {
			for _, rpc := range *stats.RpcsByMessageType {
				messageType := rpc.MessageType

				if rpc.Count > 0 {
					if !yield(prometheus.MustNewConstMetric(c.rpcCallsTotal, prometheus.CounterValue, float64(rpc.Count), messageType)) {
						return
					}
				}

				if rpc.TotalTime > 0 {
					durationSeconds := float64(rpc.TotalTime) / 1_000_000
					if !yield(prometheus.MustNewConstMetric(c.rpcDurationSecondsTotal, prometheus.CounterValue, durationSeconds, messageType)) {
						return
					}
				}
			}
		}

		if stats.RpcsByUser != nil {
			for _, userRpc := range *stats.RpcsByUser {
				user := userRpc.User
				userID := strconv.Itoa(int(userRpc.UserId))

				if userRpc.Count > 0 {
					if !yield(prometheus.MustNewConstMetric(c.rpcUserCallsTotal, prometheus.CounterValue, float64(userRpc.Count), user, userID)) {
						return
					}
				}

				if userRpc.TotalTime > 0 {
					durationSeconds := float64(userRpc.TotalTime) / 1_000_000
					if !yield(prometheus.MustNewConstMetric(c.rpcUserDurationSecondsTotal, prometheus.CounterValue, durationSeconds, user, userID)) {
						return
					}
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
