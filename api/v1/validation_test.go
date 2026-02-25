package v1_test

import (
	"testing"
)

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
