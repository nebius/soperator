package v1_test

import (
	"testing"

	v1 "nebius.ai/slurm-operator/api/v1"
)

func TestCELValidationLogic(t *testing.T) {
	tests := []struct {
		name     string
		spec     v1.SlurmClusterSpec
		expected bool
	}{
		{
			name: "no partitions - should pass",
			spec: v1.SlurmClusterSpec{
				SlurmNodes: v1.SlurmNodes{
					Worker: &v1.SlurmNodeWorker{
						SlurmNode: v1.SlurmNode{
							Size: 5, // Non-zero size is OK without NodeSetRefs
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "partitions without nodeSetRefs - should pass",
			spec: v1.SlurmClusterSpec{
				SlurmNodes: v1.SlurmNodes{
					Worker: &v1.SlurmNodeWorker{
						SlurmNode: v1.SlurmNode{
							Size: 5, // Non-zero size is OK without NodeSetRefs
						},
					},
				},
				PartitionConfiguration: v1.PartitionConfiguration{
					Partitions: []v1.Partition{{Name: "test"}},
				},
			},
			expected: true,
		},
		{
			name: "nodeSetRefs with zero worker size - should pass",
			spec: v1.SlurmClusterSpec{
				SlurmNodes: v1.SlurmNodes{
					Worker: &v1.SlurmNodeWorker{
						SlurmNode: v1.SlurmNode{
							Size: 0, // Zero size with NodeSetRefs is OK
						},
					},
				},
				PartitionConfiguration: v1.PartitionConfiguration{
					Partitions: []v1.Partition{{
						Name:        "test",
						NodeSetRefs: []string{"nodeset1"},
					}},
				},
			},
			expected: true,
		},
		{
			name: "nodeSetRefs with non-zero worker size - should fail",
			spec: v1.SlurmClusterSpec{
				SlurmNodes: v1.SlurmNodes{
					Worker: &v1.SlurmNodeWorker{
						SlurmNode: v1.SlurmNode{
							Size: 5, // Non-zero size with NodeSetRefs should fail
						},
					},
				},
				PartitionConfiguration: v1.PartitionConfiguration{
					Partitions: []v1.Partition{{
						Name:        "test",
						NodeSetRefs: []string{"nodeset1"},
					}},
				},
			},
			expected: false,
		},
		{
			name: "empty nodeSetRefs with non-zero worker - should pass",
			spec: v1.SlurmClusterSpec{
				SlurmNodes: v1.SlurmNodes{
					Worker: &v1.SlurmNodeWorker{
						SlurmNode: v1.SlurmNode{
							Size: 5,
						},
					},
				},
				PartitionConfiguration: v1.PartitionConfiguration{
					Partitions: []v1.Partition{{
						Name:        "test",
						NodeSetRefs: []string{}, // Empty NodeSetRefs
					}},
				},
			},
			expected: true,
		},
		{
			name: "nodeSetRefs with nil worker - should pass",
			spec: v1.SlurmClusterSpec{
				SlurmNodes: v1.SlurmNodes{
					Worker: nil, // No worker defined
				},
				PartitionConfiguration: v1.PartitionConfiguration{
					Partitions: []v1.Partition{{
						Name:        "test",
						NodeSetRefs: []string{"nodeset1"},
					}},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Manual implementation of the CEL logic for testing
			result := evaluateCELLogic(tt.spec)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for spec: %+v", tt.expected, result, tt.spec)
			} else {
				t.Logf("%s: Expected %v, got %v", tt.name, tt.expected, result)
			}
		})
	}
}

// evaluateCELLogic manually implements the CEL validation logic for testing
func evaluateCELLogic(spec v1.SlurmClusterSpec) bool {
	// CEL: !(has(self.partitionConfiguration) && has(self.partitionConfiguration.partitions) &&
	//       size(self.partitionConfiguration.partitions) > 0 &&
	//       self.partitionConfiguration.partitions.exists(p, size(p.nodeSetRefs) > 0) &&
	//       has(self.slurmNodes.worker) && self.slurmNodes.worker.size > 0)

	// Check if there are partitions
	if len(spec.PartitionConfiguration.Partitions) == 0 {
		return true
	}

	// Check if any partition has nodeSetRefs with size > 0
	hasNodeSetRefs := false
	for _, partition := range spec.PartitionConfiguration.Partitions {
		if len(partition.NodeSetRefs) > 0 {
			hasNodeSetRefs = true
			break
		}
	}
	if !hasNodeSetRefs {
		return true
	}

	// Check if worker exists and has size greater than zero
	if spec.SlurmNodes.Worker == nil || spec.SlurmNodes.Worker.Size == 0 {
		return true
	}

	// If we reach here, all conditions are met - this should fail validation
	return false
}

// TestCELExpressionPseudoValidation tests the logical structure we expect
func TestCELExpressionPseudoValidation(t *testing.T) {
	t.Log("Testing CEL expression logic manually:")
	t.Log("Rule: !(has(partitionConfiguration) && partitions.exists(p, size(p.nodeSetRefs) > 0) && has(worker) && size(worker) > 0)")
	t.Log("")

	// This test documents the expected behavior:
	scenarios := []struct {
		hasPartitions  bool
		hasNodeSetRefs bool
		hasWorker      bool
		workerNotEmpty bool
		shouldPass     bool
		description    string
	}{
		{false, false, false, false, true, "No partitions"},
		{true, false, false, false, true, "Partitions without nodeSetRefs"},
		{true, true, false, false, true, "NodeSetRefs but no worker"},
		{true, true, true, false, true, "NodeSetRefs with empty worker"},
		{true, true, true, true, false, "NodeSetRefs with non-empty worker - SHOULD FAIL"},
	}

	for i, scenario := range scenarios {
		t.Logf("Scenario %d: %s", i+1, scenario.description)
		t.Logf("  - Has partitions: %v", scenario.hasPartitions)
		t.Logf("  - Has nodeSetRefs: %v", scenario.hasNodeSetRefs)
		t.Logf("  - Has worker: %v", scenario.hasWorker)
		t.Logf("  - Worker not empty: %v", scenario.workerNotEmpty)
		t.Logf("  - Should pass: %v", scenario.shouldPass)
		t.Logf("")
	}
}
