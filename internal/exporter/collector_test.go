package exporter

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"nebius.ai/slurm-operator/internal/slurmapi"
	"nebius.ai/slurm-operator/internal/slurmapi/fake"
)

// Helper function to setup mocks and collect state for tests
func setupCollectorWithMockedData(t *testing.T, collector *MetricsCollector, mockClient *fake.MockClient, nodes []slurmapi.Node, jobs []slurmapi.Job, diag *api.V0041OpenapiDiagResp) {
	mockClient.EXPECT().ListNodes(mock.Anything).Return(nodes, nil)
	mockClient.EXPECT().ListJobs(mock.Anything).Return(jobs, nil)
	mockClient.EXPECT().GetDiag(mock.Anything).Return(diag, nil)

	ctx := context.Background()

	// Preserve the initial lastGPUSecondsUpdate for accurate GPU seconds calculations in tests
	initialState := collector.state.Load()
	if initialState == nil {
		initialState = newMetricsCollectorState()
	}
	preservedTime := initialState.lastGPUSecondsUpdate

	// Update the state with new data
	err := collector.updateState(ctx)
	assert.NoError(t, err)

	// Restore the preserved time for GPU seconds calculations
	currentState := collector.state.Load()
	currentState.lastGPUSecondsUpdate = preservedTime
	collector.state.Store(currentState)
}

func TestMetricsCollector_Describe(t *testing.T) {
	mockClient := &fake.MockClient{}
	collector := NewMetricsCollector(mockClient)

	ch := make(chan *prometheus.Desc, 10)
	go func() {
		collector.Describe(ch)
		close(ch)
	}()

	var descriptors []*prometheus.Desc
	for desc := range ch {
		descriptors = append(descriptors, desc)
	}

	// Should have base descriptors plus RPC metrics: nodeInfo, jobInfo, jobNode, jobDuration + 4 RPC metrics + 1 controller metric
	assert.GreaterOrEqual(t, len(descriptors), 9)

	// Verify descriptor names
	found := make(map[string]bool)
	for _, desc := range descriptors {
		found[desc.String()] = true
	}

	// Base metrics
	assert.Contains(t, found, `Desc{fqName: "slurm_node_info", help: "Slurm node info", constLabels: {}, variableLabels: {node_name,instance_id,state_base,state_is_drain,state_is_maintenance,state_is_reserved,address}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_job_info", help: "Slurm job detail information", constLabels: {}, variableLabels: {job_id,job_state,job_state_reason,slurm_partition,job_name,user_name,user_mail,user_id,standard_error,standard_output,array_job_id,array_task_id,submit_time,start_time,end_time,finished_time}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_node_job", help: "Slurm job node information", constLabels: {}, variableLabels: {job_id,node_name}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_job_duration_seconds", help: "Slurm job duration in seconds", constLabels: {}, variableLabels: {job_id}}`)

	// RPC metrics
	assert.Contains(t, found, `Desc{fqName: "slurm_controller_rpc_calls_total", help: "Total count of RPC calls by message type", constLabels: {}, variableLabels: {message_type}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_controller_rpc_duration_seconds_total", help: "Total time spent processing RPCs by message type", constLabels: {}, variableLabels: {message_type}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_controller_rpc_user_calls_total", help: "Total count of RPC calls by user", constLabels: {}, variableLabels: {user,user_id}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_controller_rpc_user_duration_seconds_total", help: "Total time spent on user RPCs", constLabels: {}, variableLabels: {user,user_id}}`)

	// Controller metrics
	assert.Contains(t, found, `Desc{fqName: "slurm_controller_server_thread_count", help: "Number of server threads", constLabels: {}, variableLabels: {}}`)
}

func TestMetricsCollector_Collect_Success(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	synctest.Run(func() {
		mockClient := &fake.MockClient{}
		collector := NewMetricsCollector(mockClient)

		// Mock successful ListNodes response
		testNodes := []slurmapi.Node{
			{
				Name:       "node-1",
				InstanceID: "instance-1",
				States: map[api.V0041NodeState]struct{}{
					api.V0041NodeStateALLOCATED: {},
				},
				Tres:    "cpu=16,mem=191356M,gres/gpu=2",
				Address: "10.0.0.1",
			},
			{
				Name:       "node-2",
				InstanceID: "instance-2",
				States: map[api.V0041NodeState]struct{}{
					api.V0041NodeStateIDLE:  {},
					api.V0041NodeStateDRAIN: {},
				},
				Tres:    "cpu=8,mem=64000M,gres/gpu=1",
				Address: "10.0.0.2",
			},
		}

		// Mock GetDiag response with realistic data
		serverThreadCount := int32(1)
		testDiag := &api.V0041OpenapiDiagResp{
			Statistics: api.V0041StatsMsg{
				ServerThreadCount: &serverThreadCount,
			},
		}

		arrayTaskID := int32(42)
		userID := int32(1000)

		// Define timestamps for the test job
		submitTime := metav1.NewTime(time.Unix(1722697200, 0)) // 2024-08-03 10:00:00 UTC
		startTime := metav1.NewTime(time.Unix(1722697230, 0))  // 2024-08-03 10:00:30 UTC

		// Mock successful ListJobs response
		testJobs := []slurmapi.Job{
			{
				ID:             12345,
				Name:           "test_job",
				State:          "RUNNING",
				StateReason:    "None",
				Partition:      "gpu",
				UserName:       "testuser",
				UserMail:       "testuser@example.com",
				UserID:         &userID,
				StandardError:  "/path/to/stderr",
				StandardOutput: "/path/to/stdout",
				Nodes:          "node-[1,2]",
				ArrayJobID:     nil,
				ArrayTaskID:    &arrayTaskID,
				SubmitTime:     &submitTime,
				StartTime:      &startTime,
				EndTime:        nil, // Job is still running
			},
		}

		// Advance time by 10 seconds to create meaningful GPU seconds values
		// GPU seconds calculation: gpuSecondsInc = timeDelta * gpuCount
		// Expected: node-1 (2 GPUs) = 20, node-2 (1 GPU) = 10
		time.Sleep(10 * time.Second)

		setupCollectorWithMockedData(t, collector, mockClient, testNodes, testJobs, testDiag)

		ch := make(chan prometheus.Metric, 20)
		go func() {
			collector.Collect(ch)
			close(ch)
		}()

		var metrics []prometheus.Metric
		for metric := range ch {
			metrics = append(metrics, metric)
		}

		// Should have at least: 2 node info + 1 job info + 1 job node + GPU seconds metrics
		assert.GreaterOrEqual(t, len(metrics), 4)

		var metricsText []string
		for _, metric := range metrics {
			metricsText = append(metricsText, toPrometheusLikeString(t, metric))
		}

		expectedMetrics := []string{
			`GAUGE; slurm_node_info{address="10.0.0.1",instance_id="instance-1",node_name="node-1",state_base="ALLOCATED",state_is_drain="false",state_is_maintenance="false",state_is_reserved="false"} 1`,
			`GAUGE; slurm_node_info{address="10.0.0.2",instance_id="instance-2",node_name="node-2",state_base="IDLE",state_is_drain="true",state_is_maintenance="false",state_is_reserved="false"} 1`,
			`COUNTER; slurm_node_gpu_seconds_total{node_name="node-1",state_base="ALLOCATED",state_is_drain="false",state_is_maintenance="false",state_is_reserved="false"} 20`,
			`COUNTER; slurm_node_gpu_seconds_total{node_name="node-2",state_base="IDLE",state_is_drain="true",state_is_maintenance="false",state_is_reserved="false"} 10`,
			`GAUGE; slurm_job_info{array_job_id="",array_task_id="42",end_time="",finished_time="",job_id="12345",job_name="test_job",job_state="RUNNING",job_state_reason="None",slurm_partition="gpu",standard_error="/path/to/stderr",standard_output="/path/to/stdout",start_time="1722697230",submit_time="1722697200",user_id="1000",user_mail="testuser@example.com",user_name="testuser"} 1`,
			`GAUGE; slurm_node_job{job_id="12345",node_name="node-1"} 1`,
			`GAUGE; slurm_node_job{job_id="12345",node_name="node-2"} 1`,
			`GAUGE; slurm_controller_server_thread_count 1`,
		}

		assert.ElementsMatch(t, expectedMetrics, metricsText)

		mockClient.AssertExpectations(t)
	})
}

func TestMetricsCollector_Collect_APIError(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	mockClient := &fake.MockClient{}
	collector := NewMetricsCollector(mockClient)

	// Mock failed ListNodes response - with early return, other APIs won't be called
	mockClient.EXPECT().ListNodes(mock.Anything).Return(nil, assert.AnError)

	// Test that updateState fails early when critical APIs fail
	ctx := context.Background()
	err := collector.updateState(ctx)
	assert.Error(t, err) // Should error - ListNodes is critical

	// Collect should return no node metrics since ListNodes failed, but might have job metrics
	ch := make(chan prometheus.Metric, 10)
	go func() {
		collector.Collect(ch)
		close(ch)
	}()

	var metrics []prometheus.Metric
	for metric := range ch {
		metrics = append(metrics, metric)
	}

	// Should have no metrics when state is empty/initial
	assert.Equal(t, 0, len(metrics))

	mockClient.AssertExpectations(t)
}

func TestMetricsCollector_NodeFails(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	synctest.Run(func() {
		mockClient := &fake.MockClient{}
		collector := NewMetricsCollector(mockClient)

		// Mock nodes with different states to test the node fails metric with new labels
		testNodes := []slurmapi.Node{
			{
				Name:       "node-maintenance",
				InstanceID: "instance-maintenance",
				States: map[api.V0041NodeState]struct{}{
					api.V0041NodeStateIDLE:        {},
					api.V0041NodeStateMAINTENANCE: {},
				},
				Tres:    "cpu=8,mem=64000M,gres/gpu=1",
				Address: "10.0.0.3",
			},
			{
				Name:       "node-reserved",
				InstanceID: "instance-reserved",
				States: map[api.V0041NodeState]struct{}{
					api.V0041NodeStateIDLE:     {},
					api.V0041NodeStateRESERVED: {},
				},
				Tres:    "cpu=8,mem=64000M,gres/gpu=1",
				Address: "10.0.0.4",
			},
		}

		serverThreadCount := int32(1)
		testDiag := &api.V0041OpenapiDiagResp{
			Statistics: api.V0041StatsMsg{
				ServerThreadCount: &serverThreadCount,
			},
		}

		setupCollectorWithMockedData(t, collector, mockClient, testNodes, []slurmapi.Job{}, testDiag)

		// First collect - establish baseline
		ch := make(chan prometheus.Metric, 20)
		go func() {
			collector.Collect(ch)
			close(ch)
		}()

		var metrics []prometheus.Metric
		for metric := range ch {
			metrics = append(metrics, metric)
		}

		var metricsText []string
		for _, metric := range metrics {
			metricsText = append(metricsText, toPrometheusLikeString(t, metric))
		}

		// Check specific state combinations for node info metrics
		expectedNodeMetrics := []string{
			`GAUGE; slurm_node_info{address="10.0.0.3",instance_id="instance-maintenance",node_name="node-maintenance",state_base="IDLE",state_is_drain="false",state_is_maintenance="true",state_is_reserved="false"} 1`,
			`GAUGE; slurm_node_info{address="10.0.0.4",instance_id="instance-reserved",node_name="node-reserved",state_base="IDLE",state_is_drain="false",state_is_maintenance="false",state_is_reserved="true"} 1`,
		}

		for _, expected := range expectedNodeMetrics {
			assert.Contains(t, metricsText, expected)
		}

		// Check that GPU seconds metrics include all the new labels
		foundMaintenanceGPU := false
		foundReservedGPU := false
		for _, metric := range metricsText {
			if strings.Contains(metric, `slurm_node_gpu_seconds_total{node_name="node-maintenance",state_base="IDLE",state_is_drain="false",state_is_maintenance="true",state_is_reserved="false"}`) {
				foundMaintenanceGPU = true
			}
			if strings.Contains(metric, `slurm_node_gpu_seconds_total{node_name="node-reserved",state_base="IDLE",state_is_drain="false",state_is_maintenance="false",state_is_reserved="true"}`) {
				foundReservedGPU = true
			}
		}
		assert.True(t, foundMaintenanceGPU, "Expected to find maintenance node GPU seconds metric with new labels")
		assert.True(t, foundReservedGPU, "Expected to find reserved node GPU seconds metric with new labels")

		// Create a new slice with the drained node to trigger a node fail with the new labels
		drainedNodes := []slurmapi.Node{
			{
				Name:       "node-maintenance",
				InstanceID: "instance-maintenance",
				States: map[api.V0041NodeState]struct{}{
					api.V0041NodeStateIDLE:        {},
					api.V0041NodeStateMAINTENANCE: {},
					api.V0041NodeStateDRAIN:       {},
				},
				Tres:    "cpu=8,mem=64000M,gres/gpu=1",
				Address: "10.0.0.3",
				Reason: &slurmapi.NodeReason{
					Reason:    "maintenance drain triggered",
					ChangedAt: time.Now(),
				},
			},
			testNodes[1], // Keep the second node unchanged
		}

		// Create a new mock for the second call to avoid mock state issues
		mockClient = &fake.MockClient{}
		mockClient.EXPECT().ListNodes(mock.Anything).Return(drainedNodes, nil)
		mockClient.EXPECT().ListJobs(mock.Anything).Return([]slurmapi.Job{}, nil)
		mockClient.EXPECT().GetDiag(mock.Anything).Return(testDiag, nil)

		// Create a new collector with the new mock for the second test phase
		oldState := collector.state.Load()
		collector = NewMetricsCollector(mockClient)
		// Copy the state from the previous collector to maintain continuity
		collector.state.Store(oldState)

		// Now call updateState to trigger node failure detection
		ctx := context.Background()
		err := collector.updateState(ctx)
		assert.NoError(t, err)

		// Second collect - should now show the node fails metric
		ch = make(chan prometheus.Metric, 20)
		go func() {
			collector.Collect(ch)
			close(ch)
		}()

		metrics = nil
		for metric := range ch {
			metrics = append(metrics, metric)
		}

		metricsText = nil
		for _, metric := range metrics {
			metricsText = append(metricsText, toPrometheusLikeString(t, metric))
		}

		// Check that node fails metric includes all the new labels
		expectedNodeFailsMetric := `COUNTER; slurm_node_fails_total{node_name="node-maintenance",reason="maintenance drain triggered",state_base="IDLE",state_is_drain="true",state_is_maintenance="true",state_is_reserved="false"} 1`
		assert.Contains(t, metricsText, expectedNodeFailsMetric)

		mockClient.AssertExpectations(t)
	})
}

func TestMetricsCollector_RPCMetrics_Success(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	synctest.Run(func() {
		mockClient := &fake.MockClient{}
		collector := NewMetricsCollector(mockClient)

		// Mock realistic RPC diagnostics data based on production output
		serverThreadCount := int32(1)
		rpcsByMessageType := api.V0041StatsMsgRpcsByType{
			{
				MessageType: "REQUEST_NODE_INFO",
				Count:       576,
				TotalTime:   61410,
			},
			{
				MessageType: "REQUEST_JOB_INFO",
				Count:       288,
				TotalTime:   30218,
			},
			{
				MessageType: "REQUEST_PING",
				Count:       414,
				TotalTime:   14239,
			},
		}
		rpcsByUser := api.V0041StatsMsgRpcsByUser{
			{
				User:      "root",
				UserId:    0,
				Count:     2423,
				TotalTime: 172774,
			},
			{
				User:      "testuser",
				UserId:    1000,
				Count:     100,
				TotalTime: 5000,
			},
		}

		testDiag := &api.V0041OpenapiDiagResp{
			Statistics: api.V0041StatsMsg{
				ServerThreadCount: &serverThreadCount,
				RpcsByMessageType: &rpcsByMessageType,
				RpcsByUser:        &rpcsByUser,
			},
		}

		setupCollectorWithMockedData(t, collector, mockClient, []slurmapi.Node{}, []slurmapi.Job{}, testDiag)

		ch := make(chan prometheus.Metric, 50)
		go func() {
			collector.Collect(ch)
			close(ch)
		}()

		var metrics []prometheus.Metric
		for metric := range ch {
			metrics = append(metrics, metric)
		}

		var metricsText []string
		for _, metric := range metrics {
			metricsText = append(metricsText, toPrometheusLikeString(t, metric))
		}

		// Verify controller metrics
		assert.Contains(t, metricsText, `GAUGE; slurm_controller_server_thread_count 1`)

		// Verify RPC calls by message type
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_calls_total{message_type="REQUEST_NODE_INFO"} 576`)
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_calls_total{message_type="REQUEST_JOB_INFO"} 288`)
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_calls_total{message_type="REQUEST_PING"} 414`)

		// Verify RPC duration by message type (converted from microseconds to seconds)
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_duration_seconds_total{message_type="REQUEST_NODE_INFO"} 0.06141`)
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_duration_seconds_total{message_type="REQUEST_JOB_INFO"} 0.030218`)
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_duration_seconds_total{message_type="REQUEST_PING"} 0.014239`)

		// Verify RPC calls by user
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_user_calls_total{user="root",user_id="0"} 2423`)
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_user_calls_total{user="testuser",user_id="1000"} 100`)

		// Verify RPC duration by user (converted from microseconds to seconds)
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_user_duration_seconds_total{user="root",user_id="0"} 0.172774`)
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_user_duration_seconds_total{user="testuser",user_id="1000"} 0.005`)

		mockClient.AssertExpectations(t)
	})
}

func TestMetricsCollector_RPCMetrics_EdgeCases(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	synctest.Run(func() {
		mockClient := &fake.MockClient{}
		collector := NewMetricsCollector(mockClient)

		serverThreadCount := int32(0)
		rpcsByMessageType := api.V0041StatsMsgRpcsByType{
			{
				MessageType: "ZERO_COUNT",
				Count:       0,
				TotalTime:   1000,
			},
			{
				MessageType: "ZERO_TIME",
				Count:       10,
				TotalTime:   0,
			},
			{
				MessageType: "NORMAL",
				Count:       1,
				TotalTime:   1,
			},
		}
		rpcsByUser := api.V0041StatsMsgRpcsByUser{
			{
				User:      "zero_user",
				UserId:    999,
				Count:     0,
				TotalTime: 5000,
			},
			{
				User:      "normal_user",
				UserId:    1001,
				Count:     1,
				TotalTime: 1,
			},
		}

		testDiag := &api.V0041OpenapiDiagResp{
			Statistics: api.V0041StatsMsg{
				ServerThreadCount: &serverThreadCount,
				RpcsByMessageType: &rpcsByMessageType,
				RpcsByUser:        &rpcsByUser,
			},
		}

		setupCollectorWithMockedData(t, collector, mockClient, []slurmapi.Node{}, []slurmapi.Job{}, testDiag)

		ch := make(chan prometheus.Metric, 50)
		go func() {
			collector.Collect(ch)
			close(ch)
		}()

		var metrics []prometheus.Metric
		for metric := range ch {
			metrics = append(metrics, metric)
		}

		var metricsText []string
		for _, metric := range metrics {
			metricsText = append(metricsText, toPrometheusLikeString(t, metric))
		}

		// Should emit zero controller thread count
		assert.Contains(t, metricsText, `GAUGE; slurm_controller_server_thread_count 0`)

		for _, metricText := range metricsText {
			if strings.Contains(metricText, `message_type="ZERO_COUNT"`) {
				assert.Contains(t, metricText, `slurm_controller_rpc_duration_seconds_total`)
			}
			if strings.Contains(metricText, `user="zero_user"`) {
				assert.Contains(t, metricText, `slurm_controller_rpc_user_duration_seconds_total`)
			}
		}

		// Should emit non-zero metrics
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_calls_total{message_type="ZERO_TIME"} 10`)
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_calls_total{message_type="NORMAL"} 1`)

		// Should NOT emit zero duration metrics
		for _, metricText := range metricsText {
			assert.NotContains(t, metricText, `slurm_controller_rpc_duration_seconds_total{message_type="ZERO_TIME"}`)
		}

		// Should emit very small duration
		assert.Contains(t, metricsText, `COUNTER; slurm_controller_rpc_duration_seconds_total{message_type="NORMAL"} 1e-06`)

		mockClient.AssertExpectations(t)
	})
}

func TestMetricsCollector_GetDiag_APIError(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	synctest.Run(func() {
		mockClient := &fake.MockClient{}
		collector := NewMetricsCollector(mockClient)

		// Mock successful node and job calls
		testNodes := []slurmapi.Node{
			{
				Name:       "test-node",
				InstanceID: "test-instance",
				States: map[api.V0041NodeState]struct{}{
					api.V0041NodeStateIDLE: {},
				},
				Tres:    "cpu=4,mem=8000M,gres/gpu=0",
				Address: "10.0.0.1",
			},
		}

		mockClient.EXPECT().ListNodes(mock.Anything).Return(testNodes, nil)
		mockClient.EXPECT().ListJobs(mock.Anything).Return([]slurmapi.Job{}, nil)
		mockClient.EXPECT().GetDiag(mock.Anything).Return(nil, assert.AnError)

		ctx := context.Background()
		err := collector.updateState(ctx)
		assert.Error(t, err) // Should fail due to GetDiag error

		// Verify that diag is nil in the state due to API error
		currentState := collector.state.Load()
		assert.NotNil(t, currentState)
		assert.Nil(t, currentState.diag) // diag should be nil due to API error

		ch := make(chan prometheus.Metric, 50)
		go func() {
			collector.Collect(ch)
			close(ch)
		}()

		var metrics []prometheus.Metric
		for metric := range ch {
			metrics = append(metrics, metric)
		}

		var metricsText []string
		for _, metric := range metrics {
			metricsText = append(metricsText, toPrometheusLikeString(t, metric))
		}

		// Should still have node metrics (proving other metrics continue to work)
		assert.Contains(t, metricsText, `GAUGE; slurm_node_info{address="10.0.0.1",instance_id="test-instance",node_name="test-node",state_base="IDLE",state_is_drain="false",state_is_maintenance="false",state_is_reserved="false"} 1`)

		// Should NOT have any RPC metrics due to GetDiag failure
		for _, metricText := range metricsText {
			assert.NotContains(t, metricText, `slurm_controller_rpc_`)
			assert.NotContains(t, metricText, `slurm_controller_server_thread_count`)
		}

		mockClient.AssertExpectations(t)
	})
}

func TestMetricsCollector_GetDiag_NilFields(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	synctest.Run(func() {
		mockClient := &fake.MockClient{}
		collector := NewMetricsCollector(mockClient)

		// Mock GetDiag response with nil fields
		testDiag := &api.V0041OpenapiDiagResp{
			Statistics: api.V0041StatsMsg{
				ServerThreadCount: nil, // Should not emit metric
				RpcsByMessageType: nil, // Should not emit metrics
				RpcsByUser:        nil, // Should not emit metrics
			},
		}

		setupCollectorWithMockedData(t, collector, mockClient, []slurmapi.Node{}, []slurmapi.Job{}, testDiag)

		ch := make(chan prometheus.Metric, 50)
		go func() {
			collector.Collect(ch)
			close(ch)
		}()

		var metrics []prometheus.Metric
		for metric := range ch {
			metrics = append(metrics, metric)
		}

		var metricsText []string
		for _, metric := range metrics {
			metricsText = append(metricsText, toPrometheusLikeString(t, metric))
		}

		// Should NOT have any RPC metrics when all fields are nil
		for _, metricText := range metricsText {
			assert.NotContains(t, metricText, `slurm_controller_rpc_`)
			assert.NotContains(t, metricText, `slurm_controller_server_thread_count`)
		}

		mockClient.AssertExpectations(t)
	})
}

// toPrometheusLikeString returns metric text representation like Prometheus does, with some extra additions.
// E.g.:
// GAUGE; slurm_node_info{address="10.0.0.1",instance_id="computeinstance-xyz",node_name="worker-0",state_base="idle",state_is_drain="false",state_is_maintenance="false",state_is_reserved="false"} 1
func TestMetricsCollector_JobMetrics_FinishedTime(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	synctest.Run(func() {
		mockClient := &fake.MockClient{}
		collector := NewMetricsCollector(mockClient)

		now := time.Now()
		submitTime := metav1.NewTime(now.Add(-60 * time.Second)) // 1 minute ago
		startTime := metav1.NewTime(now.Add(-31 * time.Second))  // 31 seconds ago
		endTime := metav1.NewTime(now)                           // now (for completed jobs)
		zeroTime := metav1.NewTime(time.Unix(0, 0))              // Unix epoch (should be treated as empty)
		userID := int32(1000)

		testJobs := []slurmapi.Job{
			{
				// Completed job - should have finished_time
				ID:             12345,
				Name:           "completed_job",
				State:          "COMPLETED",
				StateReason:    "None",
				Partition:      "gpu",
				UserName:       "testuser",
				UserMail:       "testuser@example.com",
				UserID:         &userID,
				StandardError:  "/path/to/stderr1",
				StandardOutput: "/path/to/stdout1",
				Nodes:          "node-1",
				SubmitTime:     &submitTime,
				StartTime:      &startTime,
				EndTime:        &endTime,
			},
			{
				// Running job - should NOT have finished_time
				ID:             12346,
				Name:           "running_job",
				State:          "RUNNING",
				StateReason:    "None",
				Partition:      "cpu",
				UserName:       "testuser",
				UserMail:       "testuser@example.com",
				UserID:         &userID,
				StandardError:  "/path/to/stderr2",
				StandardOutput: "/path/to/stdout2",
				Nodes:          "node-2",
				SubmitTime:     &submitTime,
				StartTime:      &startTime,
				EndTime:        &endTime, // Future forecast end time
			},
			{
				// Failed job with zero EndTime - should NOT have finished_time
				ID:             12347,
				Name:           "failed_job_no_end",
				State:          "FAILED",
				StateReason:    "OutOfMemory",
				Partition:      "cpu",
				UserName:       "testuser",
				UserMail:       "testuser@example.com",
				UserID:         &userID,
				StandardError:  "/path/to/stderr3",
				StandardOutput: "/path/to/stdout3",
				Nodes:          "node-3",
				SubmitTime:     &submitTime,
				StartTime:      &startTime,
				EndTime:        &zeroTime,
			},
			{
				// Job with zero times - should have empty time strings
				ID:             12348,
				Name:           "pending_job",
				State:          "PENDING",
				StateReason:    "Resources",
				Partition:      "cpu",
				UserName:       "testuser",
				UserMail:       "testuser@example.com",
				UserID:         &userID,
				StandardError:  "/path/to/stderr4",
				StandardOutput: "/path/to/stdout4",
				Nodes:          "",
				SubmitTime:     &submitTime,
				StartTime:      &zeroTime,
				EndTime:        &zeroTime,
			},
		}

		// Mock GetDiag response
		serverThreadCount := int32(1)
		testDiag := &api.V0041OpenapiDiagResp{
			Statistics: api.V0041StatsMsg{
				ServerThreadCount: &serverThreadCount,
			},
		}

		setupCollectorWithMockedData(t, collector, mockClient, []slurmapi.Node{}, testJobs, testDiag)

		ch := make(chan prometheus.Metric, 20)
		go func() {
			collector.Collect(ch)
			close(ch)
		}()

		var metrics []prometheus.Metric
		for metric := range ch {
			metrics = append(metrics, metric)
		}

		var metricsText []string
		for _, metric := range metrics {
			metricsText = append(metricsText, toPrometheusLikeString(t, metric))
		}

		submitTimeStr := strconv.FormatInt(submitTime.Unix(), 10)
		startTimeStr := strconv.FormatInt(startTime.Unix(), 10)
		endTimeStr := strconv.FormatInt(endTime.Unix(), 10)

		expectedJobMetrics := []string{
			// Completed job has finished_time
			fmt.Sprintf(`GAUGE; slurm_job_info{array_job_id="",array_task_id="",end_time="%s",finished_time="%s",job_id="12345",job_name="completed_job",job_state="COMPLETED",job_state_reason="None",slurm_partition="gpu",standard_error="/path/to/stderr1",standard_output="/path/to/stdout1",start_time="%s",submit_time="%s",user_id="1000",user_mail="testuser@example.com",user_name="testuser"} 1`, endTimeStr, endTimeStr, startTimeStr, submitTimeStr),
			// Running job has no finished_time
			fmt.Sprintf(`GAUGE; slurm_job_info{array_job_id="",array_task_id="",end_time="%s",finished_time="",job_id="12346",job_name="running_job",job_state="RUNNING",job_state_reason="None",slurm_partition="cpu",standard_error="/path/to/stderr2",standard_output="/path/to/stdout2",start_time="%s",submit_time="%s",user_id="1000",user_mail="testuser@example.com",user_name="testuser"} 1`, endTimeStr, startTimeStr, submitTimeStr),
			// Failed job with zero EndTime has no finished_time and empty end_time
			fmt.Sprintf(`GAUGE; slurm_job_info{array_job_id="",array_task_id="",end_time="",finished_time="",job_id="12347",job_name="failed_job_no_end",job_state="FAILED",job_state_reason="OutOfMemory",slurm_partition="cpu",standard_error="/path/to/stderr3",standard_output="/path/to/stdout3",start_time="%s",submit_time="%s",user_id="1000",user_mail="testuser@example.com",user_name="testuser"} 1`, startTimeStr, submitTimeStr),
			// Pending job has empty start_time and end_time
			fmt.Sprintf(`GAUGE; slurm_job_info{array_job_id="",array_task_id="",end_time="",finished_time="",job_id="12348",job_name="pending_job",job_state="PENDING",job_state_reason="Resources",slurm_partition="cpu",standard_error="/path/to/stderr4",standard_output="/path/to/stdout4",start_time="",submit_time="%s",user_id="1000",user_mail="testuser@example.com",user_name="testuser"} 1`, submitTimeStr),
		}

		for _, expected := range expectedJobMetrics {
			assert.Contains(t, metricsText, expected)
		}

		assert.Contains(t, metricsText, `GAUGE; slurm_job_duration_seconds{job_id="12345"} 31`)
		// Running job should have duration > 30 seconds (started 31 seconds ago, so duration will be 31 seconds)
		foundRunningDuration := false
		for _, metric := range metricsText {
			if strings.Contains(metric, `slurm_job_duration_seconds{job_id="12346"}`) {
				foundRunningDuration = true
				// Extract the duration value
				parts := strings.Split(metric, " ")
				if len(parts) == 3 {
					duration, err := strconv.ParseFloat(parts[2], 64)
					assert.NoError(t, err)
					assert.Greater(t, duration, 30.0, "Running job duration should be > 30 seconds")
				}
			}
		}
		assert.True(t, foundRunningDuration, "Should find duration metric for running job")
		// Failed job with zero end time should NOT have duration metric (terminal state without valid end time)
		for _, metric := range metricsText {
			assert.NotContains(t, metric, `slurm_job_duration_seconds{job_id="12347"}`)
		}
		// Pending job with zero start time should NOT have duration metric
		for _, metric := range metricsText {
			assert.NotContains(t, metric, `slurm_job_duration_seconds{job_id="12348"}`)
		}

		mockClient.AssertExpectations(t)
	})
}

func toPrometheusLikeString(t *testing.T, metric prometheus.Metric) string {
	var pb dto.Metric
	if err := metric.Write(&pb); err != nil {
		return fmt.Sprintf("getting metric dto: %v", err)
	}

	desc := metric.Desc()
	metricName := desc.String()

	parts := strings.Split(metricName, `"`)
	if len(parts) >= 3 {
		// The metric name is typically the second quoted string, e.g.:
		// Desc{fqName: "soperator_cluster_info", help: "Soperator cluster information", constLabels: {soperator_version="test-version"}, variableLabels: {}}
		metricName = parts[1]
	}

	var metricType string
	var value float64

	switch {
	case pb.GetGauge() != nil:
		metricType = "GAUGE"
		value = pb.GetGauge().GetValue()
	case pb.GetCounter() != nil:
		metricType = "COUNTER"
		value = pb.GetCounter().GetValue()
	default:
		t.Fatalf("metric %q has unexpected type", metricName)
	}

	var labelPairs []string
	for _, labelPair := range pb.GetLabel() {
		labelPairs = append(labelPairs, fmt.Sprintf(`%s="%s"`,
			labelPair.GetName(), labelPair.GetValue()))
	}

	labelsString := ""
	if len(labelPairs) > 0 {
		labelsString = "{" + strings.Join(labelPairs, ",") + "}"
	}

	return fmt.Sprintf("%s; %s%s %g", metricType, metricName, labelsString, value)
}

func TestMetricsCollector_WithMonitoringMetrics(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	synctest.Run(func() {
		mockClient := &fake.MockClient{}
		collector := NewMetricsCollector(mockClient)

		// Mock successful response
		testNodes := []slurmapi.Node{
			{
				Name:       "node-1",
				InstanceID: "instance-1",
				Address:    "10.0.0.1",
				States: map[api.V0041NodeState]struct{}{
					api.V0041NodeStateIDLE: {},
				},
				Tres: "cpu=8,mem=32000,billing=8,gres/gpu=2",
			},
		}

		testJobs := []slurmapi.Job{
			{
				ID:    123,
				Name:  "test-job",
				State: "RUNNING",
			},
		}

		serverThreadCount := int32(1)
		testDiag := &api.V0041OpenapiDiagResp{
			Statistics: api.V0041StatsMsg{
				ServerThreadCount: &serverThreadCount,
			},
		}

		// Setup mocks for successful collection
		mockClient.EXPECT().ListNodes(mock.Anything).Return(testNodes, nil)
		mockClient.EXPECT().ListJobs(mock.Anything).Return(testJobs, nil)
		mockClient.EXPECT().GetDiag(mock.Anything).Return(testDiag, nil)

		ctx := context.Background()

		// Test successful collection
		err := collector.updateState(ctx)
		assert.NoError(t, err)

		// Test failed collection - create a new mock client to avoid call conflicts
		mockClientFail := &fake.MockClient{}
		collector.slurmAPIClient = mockClientFail
		mockClientFail.EXPECT().ListNodes(mock.Anything).Return(nil, errors.New("API error"))
		err = collector.updateState(ctx)
		assert.Error(t, err)

		// Create registry to check monitoring metrics
		registry := prometheus.NewRegistry()
		require.NoError(t, collector.Monitoring.Register(registry))

		// Collect metrics to trigger metric counting
		metricsChan := make(chan prometheus.Metric, 100)
		go func() {
			collector.Collect(metricsChan)
			close(metricsChan)
		}()

		var metricsCount int
		for range metricsChan {
			metricsCount++
		}

		// Verify monitoring metrics
		metricFamilies, err := registry.Gather()
		require.NoError(t, err)

		var attemptsTotal, failuresTotal, exportedCount float64

		for _, mf := range metricFamilies {
			if len(mf.Metric) == 0 {
				continue
			}
			switch *mf.Name {
			case "slurm_exporter_collection_attempts_total":
				attemptsTotal = *mf.Metric[0].Counter.Value
			case "slurm_exporter_collection_failures_total":
				failuresTotal = *mf.Metric[0].Counter.Value
			case "slurm_exporter_metrics_exported":
				exportedCount = *mf.Metric[0].Gauge.Value
			}
		}

		t.Logf("Monitoring metrics: attempts=%f, failures=%f, exported=%f, collected=%d", attemptsTotal, failuresTotal, exportedCount, metricsCount)
		assert.Equal(t, float64(2), attemptsTotal, "Expected 2 collection attempts (1 success + 1 failure)")
		assert.Equal(t, float64(1), failuresTotal, "Expected 1 collection failure")
		assert.Greater(t, exportedCount, float64(0), "Expected some metrics to be exported")
		assert.Greater(t, metricsCount, 0, "Expected some metrics to be collected")
	})
}
