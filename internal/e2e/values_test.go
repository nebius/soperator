package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOverrideTestValuesNFS(t *testing.T) {
	for _, tc := range []struct {
		name     string
		version  string
		unstable bool
		enabled  bool
	}{
		{name: "candidate", version: "1.2.2-r2012dad5", unstable: true, enabled: true},
		{name: "stable", version: "1.2.2", enabled: true},
		{name: "disabled", version: "1.2.2-r2012dad5", unstable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nfs := map[string]interface{}{
				"enabled": tc.enabled,
				"spec": map[string]interface{}{
					"version": "1.2.0", "use_stable_repo": true,
					"size_gibibytes": 3720, "threads": 32, "disk_type": "NETWORK_SSD_IO_M3",
					"node_group": map[string]interface{}{"resource": map[string]interface{}{"platform": "cpu-d3"}},
				},
			}
			result := overrideTestValues(map[string]interface{}{"nfs_in_k8s": nfs}, Config{
				SoperatorVersion: "4.1.12-r2012dad5", SoperatorUnstable: tc.unstable, NFSVersion: tc.version,
			})
			assert.Equal(t, map[string]interface{}{
				"enabled": tc.enabled,
				"spec": map[string]interface{}{
					"version": tc.version, "use_stable_repo": !tc.unstable,
					"size_gibibytes": 3720, "threads": 32, "disk_type": "NETWORK_SSD_IO_M3",
					"node_group": map[string]interface{}{"resource": map[string]interface{}{"platform": "cpu-d3"}},
				},
			}, result["nfs_in_k8s"])
		})
	}
}

func TestOverrideTestValuesWithoutNFS(t *testing.T) {
	result := overrideTestValues(map[string]interface{}{}, Config{NFSVersion: "1.2.2"})
	assert.NotContains(t, result, "nfs_in_k8s")
	result = overrideTestValues(map[string]interface{}{"nfs_in_k8s": nil}, Config{NFSVersion: "1.2.2"})
	assert.Nil(t, result["nfs_in_k8s"])
	for _, nfs := range []map[string]interface{}{
		{"enabled": false},
		{"enabled": false, "spec": nil},
	} {
		expected := map[string]interface{}{}
		for key, value := range nfs {
			expected[key] = value
		}
		result = overrideTestValues(map[string]interface{}{"nfs_in_k8s": nfs}, Config{NFSVersion: "1.2.2"})
		assert.Equal(t, expected, result["nfs_in_k8s"])
	}
}

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
