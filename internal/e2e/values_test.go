package e2e

import "testing"

func TestOverrideTestValuesCreatesWorkerPartitions(t *testing.T) {
	tfVars := map[string]interface{}{}
	cfg := Config{
		Profile: Profile{
			NodeSets: []NodeSetDef{
				{Name: "cpu", Platform: "cpu-d3", Preset: "16vcpu-64gb", Size: 1},
				{Name: "gpu", Platform: "gpu-h100-sxm", Preset: "8gpu-128vcpu-1600gb", Size: 2},
			},
		},
	}

	got := overrideTestValues(tfVars, cfg)
	workers, ok := got["slurm_nodeset_workers"].([]interface{})
	if !ok {
		t.Fatalf("slurm_nodeset_workers has type %T, want []interface{}", got["slurm_nodeset_workers"])
	}

	for i, worker := range workers {
		values, ok := worker.(map[string]interface{})
		if !ok {
			t.Fatalf("slurm_nodeset_workers[%d] has type %T, want map[string]interface{}", i, worker)
		}
		if create, ok := values["create_partition"].(bool); !ok || !create {
			t.Errorf("slurm_nodeset_workers[%d].create_partition = %#v, want true", i, values["create_partition"])
		}
	}
}
