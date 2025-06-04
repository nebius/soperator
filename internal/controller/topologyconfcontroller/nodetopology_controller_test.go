package topologyconfcontroller_test

import (
	"reflect"
	"testing"

	"nebius.ai/slurm-operator/internal/consts"
	tc "nebius.ai/slurm-operator/internal/controller/topologyconfcontroller"
)

func TestExtractTierLabels(t *testing.T) {
	// Test data
	k8sNodeLabels := map[string]string{
		consts.TopologyLabelPrefix + "/tier-1": "leaf00",
		consts.TopologyLabelPrefix + "/other":  "value",
		consts.TopologyLabelPrefix + "/tier-2": "spine00",
		"unrelated.label":                      "unrelatedValue",
	}

	// Expected result
	expected := map[string]string{
		"tier-1": "leaf00",
		"tier-2": "spine00",
	}

	// Call the function
	result := tc.ExtractTierLabels(k8sNodeLabels, consts.TopologyLabelPrefix)

	// Validate the result
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("ExtractTierLabels() = %v, want %v", result, expected)
	}
}
