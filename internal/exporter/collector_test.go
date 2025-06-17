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

	"nebius.ai/slurm-operator/internal/slurmapi"
	"nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func TestMetricsCollector_Describe(t *testing.T) {
	mockClient := &fake.MockClient{}
	collector := NewMetricsCollector(mockClient, "test-version")

	ch := make(chan *prometheus.Desc, 10)
	go func() {
		collector.Describe(ch)
		close(ch)
	}()

	var descriptors []*prometheus.Desc
	for desc := range ch {
		descriptors = append(descriptors, desc)
	}

	// Should have 4 descriptors: sopClusterInfo, nodeInfo, jobInfo, jobNode
	// Plus the nodeGPUSeconds descriptor
	assert.GreaterOrEqual(t, len(descriptors), 4)

	// Verify descriptor names
	found := make(map[string]bool)
	for _, desc := range descriptors {
		found[desc.String()] = true
	}

	assert.Contains(t, found, `Desc{fqName: "soperator_cluster_info", help: "Soperator cluster information", constLabels: {soperator_version="test-version"}, variableLabels: {}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_node_info", help: "Slurm node info", constLabels: {}, variableLabels: {node_name,compute_instance_id,base_state,is_drain,address}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_job_info", help: "Slurm job detail information", constLabels: {}, variableLabels: {job_id,job_state,job_state_reason,slurm_partition,job_name,user_name,standard_error,standard_output,array_job_id,array_task_id}}`)
	assert.Contains(t, found, `Desc{fqName: "slurm_node_job", help: "Slurm job node information", constLabels: {}, variableLabels: {job_id,node_name}}`)
}

func TestMetricsCollector_Collect_Success(t *testing.T) {
	synctest.Run(func() {
		mockClient := &fake.MockClient{}
		collector := NewMetricsCollector(mockClient, "test-version")
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

		// Should have at least: 1 cluster info + 2 node info + 1 job info + 1 job node + GPU seconds metrics
		assert.GreaterOrEqual(t, len(metrics), 5)

		var metricsText []string
		for _, metric := range metrics {
			metricsText = append(metricsText, toPrometheusLikeString(t, metric))
		}

		expectedMetrics := []string{
			`GAUGE; soperator_cluster_info{soperator_version="test-version"} 1`,
			`GAUGE; slurm_node_info{address="10.0.0.1",base_state="ALLOCATED",compute_instance_id="instance-1",is_drain="false",node_name="node-1"} 1`,
			`GAUGE; slurm_node_info{address="10.0.0.2",base_state="IDLE",compute_instance_id="instance-2",is_drain="true",node_name="node-2"} 1`,
			`COUNTER; slurm_active_node_gpu_seconds_total{node_name="node-1"} 20`, // (10 seconds * 2 gpu on node-1) = 20 passed.
			`COUNTER; slurm_job_alloc_gpu_seconds_total 30`,                       // 10 seconds * 3 GPUs for a job on both nodes.
			`GAUGE; slurm_job_info{array_job_id="",array_task_id="42",job_id="12345",job_name="test_job",job_state="RUNNING",job_state_reason="None",slurm_partition="gpu",standard_error="/path/to/stderr",standard_output="/path/to/stdout",user_name="testuser"} 1`,
			`GAUGE; slurm_node_job{job_id="12345",node_name="node-1"} 1`,
			`GAUGE; slurm_node_job{job_id="12345",node_name="node-2"} 1`,
		}

		assert.Equal(t, expectedMetrics, metricsText)

		// Now drain node-0 and check that slurm_node_fails_total appear in the metrics.
		testNodes[0].States = map[slurmapispec.V0041NodeState]struct{}{
			slurmapispec.V0041NodeStateDRAIN: {},
		}
		testNodes[0].Reason = &slurmapi.NodeReason{
			Reason:    "state changed to drain",
			ChangedAt: time.Now(),
		}
		mockClient.EXPECT().ListNodes(mock.Anything).Return(testNodes, nil)
		mockClient.EXPECT().ListJobs(mock.Anything).Return(testJobs, nil)
		ch = make(chan prometheus.Metric, 10)
		go func() {
			collector.Collect(ch)
			close(ch)
		}()
		metrics = nil
		for metric := range ch {
			metrics = append(metrics, metric)
		}
		assert.Greater(t, len(metrics), 4)
		metricsText = nil
		for _, metric := range metrics {
			metricsText = append(metricsText, toPrometheusLikeString(t, metric))
		}
		wantSlurmNodeFailsTotal := `COUNTER; slurm_node_fails_total{node_name="node-1",reason="state changed to drain"} 1`
		assert.Contains(t, metricsText, wantSlurmNodeFailsTotal)

		mockClient.AssertExpectations(t)
	})
}

func TestMetricsCollector_Collect_APIError(t *testing.T) {
	mockClient := &fake.MockClient{}
	collector := NewMetricsCollector(mockClient, "test-version")

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

	// Should still have cluster info metric even if API fails
	assert.Equal(t, 1, len(metrics))

	mockClient.AssertExpectations(t)
}

// toPrometheusLikeString returns metric text representation like Prometheus does, with some extra additions.
// E.g.:
// GAUGE; slurm_node_info{base_state="idle",compute_instance_id="computeinstance-xyz",is_drain="false",node_name="worker-0",reason=""} 1
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
