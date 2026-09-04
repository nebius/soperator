package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderedWorker(t *testing.T, got map[string]interface{}, index int) map[string]interface{} {
	t.Helper()

	workers, ok := got["slurm_nodeset_workers"].([]interface{})
	require.Truef(t, ok, "slurm_nodeset_workers has type %T, want []interface{}", got["slurm_nodeset_workers"])
	require.Greater(t, len(workers), index)

	worker, ok := workers[index].(map[string]interface{})
	require.Truef(t, ok, "slurm_nodeset_workers[%d] has type %T, want map[string]interface{}", index, workers[index])
	return worker
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
	assert.Equal(t, true, renderedWorker(t, got, 0)["create_partition"])
	assert.Equal(t, true, renderedWorker(t, got, 1)["create_partition"])
}

func TestOverrideTestValuesRendersGB300Overrides(t *testing.T) {
	nvlink := map[string]interface{}{
		"enabled": true,
		"type":    "GB300",
	}
	placementPolicyNodes := []interface{}{"provider-node-1", "provider-node-2"}
	localNVMe := map[string]interface{}{
		"enabled":                   true,
		"device_count":              4,
		"device_capacity_gigabytes": 7680,
	}
	cfg := Config{
		Profile: Profile{
			NodeSets: []NodeSetDef{
				{
					Name:             "worker",
					Platform:         "gpu-gb300",
					Preset:           "4gpu-112vcpu-800gb",
					Size:             2,
					InfinibandFabric: "gb300-fabric",
					TerraformOverrides: map[string]interface{}{
						"nvlink":                 nvlink,
						"placement_policy_nodes": placementPolicyNodes,
						"local_nvme":             localNVMe,
					},
				},
			},
		},
	}

	got := overrideTestValues(map[string]interface{}{}, cfg)
	worker := renderedWorker(t, got, 0)
	assert.Equal(t, "worker", worker["name"])
	assert.Equal(t, 2, worker["size"])
	assert.Equal(t, map[string]interface{}{
		"platform": "gpu-gb300",
		"preset":   "4gpu-112vcpu-800gb",
	}, worker["resource"])
	assert.Equal(t, map[string]interface{}{
		"infiniband_fabric": "gb300-fabric",
	}, worker["gpu_cluster"])
	assert.Equal(t, nvlink, worker["nvlink"])
	assert.Equal(t, placementPolicyNodes, worker["placement_policy_nodes"])
	assert.Equal(t, localNVMe, worker["local_nvme"])
	assert.Equal(t, false, got["production"])
}

func TestOverrideTestValuesTopLevelOverrideReplacesGeneratedValue(t *testing.T) {
	resourceOverride := map[string]interface{}{
		"platform": "replacement-platform",
	}
	imageDiskOverride := map[string]interface{}{
		"enabled": false,
	}
	cfg := Config{
		Profile: Profile{
			NodeSets: []NodeSetDef{
				{
					Name:     "worker",
					Platform: "generated-platform",
					Preset:   "generated-preset",
					Size:     1,
					TerraformOverrides: map[string]interface{}{
						"resource":              resourceOverride,
						"node_local_image_disk": imageDiskOverride,
					},
				},
			},
		},
	}

	worker := renderedWorker(t, overrideTestValues(map[string]interface{}{}, cfg), 0)
	assert.Equal(t, resourceOverride, worker["resource"])
	assert.Equal(t, imageDiskOverride, worker["node_local_image_disk"])
}

func TestOverrideTestValuesLegacyGPUWorkersDoNotGainGB300Blocks(t *testing.T) {
	tests := []struct {
		name     string
		platform string
	}{
		{name: "H100", platform: "gpu-h100-sxm"},
		{name: "H200", platform: "gpu-h200-sxm"},
		{name: "B200", platform: "gpu-b200-sxm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Profile: Profile{
					NodeSets: []NodeSetDef{
						{Name: "worker", Platform: tt.platform, Preset: "8gpu-128vcpu-1600gb", Size: 2},
					},
				},
			}

			worker := renderedWorker(t, overrideTestValues(map[string]interface{}{}, cfg), 0)
			assert.NotContains(t, worker, "nvlink")
			assert.NotContains(t, worker, "placement_policy_nodes")
			assert.NotContains(t, worker, "local_nvme")
			assert.Equal(t, map[string]interface{}{
				"platform": tt.platform,
				"preset":   "8gpu-128vcpu-1600gb",
			}, worker["resource"])
		})
	}
}
