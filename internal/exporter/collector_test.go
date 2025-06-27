package exporter

import (
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"nebius.ai/slurm-operator/internal/slurmapi"
	"nebius.ai/slurm-operator/internal/slurmapi/fake"
)

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

	// Should have base descriptors plus RPC metrics: nodeInfo, jobInfo, jobNode + 4 RPC metrics + 1 controller metric
	assert.GreaterOrEqual(t, len(descriptors), 8)

	// Verify descriptor names
	found := make(map[string]bool)
	for _, desc := range descriptors {
		found[desc.String()] = true
	}

	// Base metrics
	assert.Contains(t, found, `Desc{fqName: "slurm_node_info", help: "Slurm node info", constLabels: {}, variableLabels: {node_name,instance_id,state_base,state_is_drain,state_is_maintenance,state_is_reserved,address}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_job_info", help: "Slurm job detail information", constLabels: {}, variableLabels: {job_id,job_state,job_state_reason,slurm_partition,job_name,user_name,standard_error,standard_output,array_job_id,array_task_id}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_node_job", help: "Slurm job node information", constLabels: {}, variableLabels: {job_id,node_name}}`)

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
		time.Sleep(time.Second * 10)

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

		mockClient.EXPECT().ListNodes(mock.Anything).Return(testNodes, nil)

		// Mock GetDiag response with realistic data
		serverThreadCount := int32(1)
		mockClient.EXPECT().GetDiag(mock.Anything).Return(&api.V0041OpenapiDiagResp{
			Statistics: api.V0041StatsMsg{
				ServerThreadCount: &serverThreadCount,
			},
		}, nil)

		arrayTaskID := int32(42)
		// Mock successful ListJobs response
		testJobs := []slurmapi.Job{
			{
				ID:             12345,
				Name:           "test_job",
				State:          "RUNNING",
				StateReason:    "None",
				Partition:      "gpu",
				UserName:       "testuser",
				StandardError:  "/path/to/stderr",
				StandardOutput: "/path/to/stdout",
				Nodes:          "node-[1,2]",
				ArrayJobID:     nil,
				ArrayTaskID:    &arrayTaskID,
			},
		}
		mockClient.EXPECT().ListJobs(mock.Anything).Return(testJobs, nil)

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
			`GAUGE; slurm_job_info{array_job_id="",array_task_id="42",job_id="12345",job_name="test_job",job_state="RUNNING",job_state_reason="None",slurm_partition="gpu",standard_error="/path/to/stderr",standard_output="/path/to/stdout",user_name="testuser"} 1`,
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

	// Mock failed ListNodes response
	mockClient.EXPECT().ListNodes(mock.Anything).Return(nil, assert.AnError)

	ch := make(chan prometheus.Metric, 10)
	go func() {
		collector.Collect(ch)
		close(ch)
	}()

	var metrics []prometheus.Metric
	for metric := range ch {
		metrics = append(metrics, metric)
	}

	// Should have no metrics if API fails
	assert.Equal(t, 0, len(metrics))

	mockClient.AssertExpectations(t)
}

func TestMetricsCollector_NodeFails(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

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
	mockClient.EXPECT().ListNodes(mock.Anything).Return(testNodes, nil)
	mockClient.EXPECT().ListJobs(mock.Anything).Return([]slurmapi.Job{}, nil)
	mockClient.EXPECT().GetDiag(mock.Anything).Return(&api.V0041OpenapiDiagResp{
		Statistics: api.V0041StatsMsg{
			ServerThreadCount: &serverThreadCount,
		},
	}, nil)

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

	// Now change one node to drain state to trigger a node fail with the new labels
	testNodes[0].States = map[api.V0041NodeState]struct{}{
		api.V0041NodeStateIDLE:        {},
		api.V0041NodeStateMAINTENANCE: {},
		api.V0041NodeStateDRAIN:       {},
	}
	testNodes[0].Reason = &slurmapi.NodeReason{
		Reason:    "maintenance drain triggered",
		ChangedAt: time.Now(),
	}

	mockClient.EXPECT().ListNodes(mock.Anything).Return(testNodes, nil)
	mockClient.EXPECT().ListJobs(mock.Anything).Return([]slurmapi.Job{}, nil)
	mockClient.EXPECT().GetDiag(mock.Anything).Return(&api.V0041OpenapiDiagResp{
		Statistics: api.V0041StatsMsg{
			ServerThreadCount: &serverThreadCount,
		},
	}, nil)

	// Second collect - trigger node fails
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
}

func TestMetricsCollector_RPCMetrics_Success(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	mockClient := &fake.MockClient{}
	collector := NewMetricsCollector(mockClient)

	// Mock successful ListNodes and ListJobs with minimal data
	mockClient.EXPECT().ListNodes(mock.Anything).Return([]slurmapi.Node{}, nil)
	mockClient.EXPECT().ListJobs(mock.Anything).Return([]slurmapi.Job{}, nil)

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

	mockClient.EXPECT().GetDiag(mock.Anything).Return(&api.V0041OpenapiDiagResp{
		Statistics: api.V0041StatsMsg{
			ServerThreadCount: &serverThreadCount,
			RpcsByMessageType: &rpcsByMessageType,
			RpcsByUser:        &rpcsByUser,
		},
	}, nil)

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
}

func TestMetricsCollector_RPCMetrics_EdgeCases(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	mockClient := &fake.MockClient{}
	collector := NewMetricsCollector(mockClient)

	// Mock minimal required calls
	mockClient.EXPECT().ListNodes(mock.Anything).Return([]slurmapi.Node{}, nil)
	mockClient.EXPECT().ListJobs(mock.Anything).Return([]slurmapi.Job{}, nil)

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

	mockClient.EXPECT().GetDiag(mock.Anything).Return(&api.V0041OpenapiDiagResp{
		Statistics: api.V0041StatsMsg{
			ServerThreadCount: &serverThreadCount,
			RpcsByMessageType: &rpcsByMessageType,
			RpcsByUser:        &rpcsByUser,
		},
	}, nil)

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
}

func TestMetricsCollector_GetDiag_APIError(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

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

	// Mock GetDiag failure
	mockClient.EXPECT().GetDiag(mock.Anything).Return(nil, assert.AnError)

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
}

func TestMetricsCollector_GetDiag_NilFields(t *testing.T) {
	log.SetLogger(zap.New(zap.UseDevMode(true)))

	mockClient := &fake.MockClient{}
	collector := NewMetricsCollector(mockClient)

	// Mock minimal required calls
	mockClient.EXPECT().ListNodes(mock.Anything).Return([]slurmapi.Node{}, nil)
	mockClient.EXPECT().ListJobs(mock.Anything).Return([]slurmapi.Job{}, nil)

	// Mock GetDiag response with nil fields
	mockClient.EXPECT().GetDiag(mock.Anything).Return(&api.V0041OpenapiDiagResp{
		Statistics: api.V0041StatsMsg{
			ServerThreadCount: nil, // Should not emit metric
			RpcsByMessageType: nil, // Should not emit metrics
			RpcsByUser:        nil, // Should not emit metrics
		},
	}, nil)

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
}

// toPrometheusLikeString returns metric text representation like Prometheus does, with some extra additions.
// E.g.:
// GAUGE; slurm_node_info{address="10.0.0.1",instance_id="computeinstance-xyz",node_name="worker-0",state_base="idle",state_is_drain="false",state_is_maintenance="false",state_is_reserved="false"} 1
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
