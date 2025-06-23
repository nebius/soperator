package exporter

import (
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	slurmapispec "github.com/SlinkyProject/slurm-client/api/v0041"
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

	// Should have 3 descriptors: nodeInfo, jobInfo, jobNode
	// Plus the nodeGPUSeconds descriptor
	assert.GreaterOrEqual(t, len(descriptors), 3)

	// Verify descriptor names
	found := make(map[string]bool)
	for _, desc := range descriptors {
		found[desc.String()] = true
	}

	assert.Contains(t, found, `Desc{fqName: "slurm_node_info", help: "Slurm node info", constLabels: {}, variableLabels: {node_name,instance_id,state_base,state_is_drain,state_is_maintenance,state_is_reserved,address}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_job_info", help: "Slurm job detail information", constLabels: {}, variableLabels: {job_id,job_state,job_state_reason,slurm_partition,job_name,user_name,standard_error,standard_output,array_job_id,array_task_id}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_node_job", help: "Slurm job node information", constLabels: {}, variableLabels: {job_id,node_name}}`)
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
				States: map[slurmapispec.V0041NodeState]struct{}{
					slurmapispec.V0041NodeStateALLOCATED: {},
				},
				Tres:    "cpu=16,mem=191356M,gres/gpu=2",
				Address: "10.0.0.1",
			},
			{
				Name:       "node-2",
				InstanceID: "instance-2",
				States: map[slurmapispec.V0041NodeState]struct{}{
					slurmapispec.V0041NodeStateIDLE:  {},
					slurmapispec.V0041NodeStateDRAIN: {},
				},
				Tres:    "cpu=8,mem=64000M,gres/gpu=1",
				Address: "10.0.0.2",
			},
		}

		mockClient.EXPECT().ListNodes(mock.Anything).Return(testNodes, nil)

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
			States: map[slurmapispec.V0041NodeState]struct{}{
				slurmapispec.V0041NodeStateIDLE:        {},
				slurmapispec.V0041NodeStateMAINTENANCE: {},
			},
			Tres:    "cpu=8,mem=64000M,gres/gpu=1",
			Address: "10.0.0.3",
		},
		{
			Name:       "node-reserved",
			InstanceID: "instance-reserved",
			States: map[slurmapispec.V0041NodeState]struct{}{
				slurmapispec.V0041NodeStateIDLE:     {},
				slurmapispec.V0041NodeStateRESERVED: {},
			},
			Tres:    "cpu=8,mem=64000M,gres/gpu=1",
			Address: "10.0.0.4",
		},
	}

	mockClient.EXPECT().ListNodes(mock.Anything).Return(testNodes, nil)
	mockClient.EXPECT().ListJobs(mock.Anything).Return([]slurmapi.Job{}, nil)

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
	testNodes[0].States = map[slurmapispec.V0041NodeState]struct{}{
		slurmapispec.V0041NodeStateIDLE:        {},
		slurmapispec.V0041NodeStateMAINTENANCE: {},
		slurmapispec.V0041NodeStateDRAIN:       {},
	}
	testNodes[0].Reason = &slurmapi.NodeReason{
		Reason:    "maintenance drain triggered",
		ChangedAt: time.Now(),
	}

	mockClient.EXPECT().ListNodes(mock.Anything).Return(testNodes, nil)
	mockClient.EXPECT().ListJobs(mock.Anything).Return([]slurmapi.Job{}, nil)

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
