package e2e

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverrideTestValuesMixedProfile(t *testing.T) {
	p := mixedGPUProfile()
	vars := overrideTestValues(map[string]interface{}{}, Config{Profile: p})
	data, err := json.Marshal(vars)
	require.NoError(t, err)
	var rendered struct {
		Workers []struct {
			Name     string `json:"name"`
			Size     int    `json:"size"`
			Resource struct {
				Platform string `json:"platform"`
				Preset   string `json:"preset"`
			} `json:"resource"`
			GPUCluster *struct {
				Fabric string `json:"infiniband_fabric"`
			} `json:"gpu_cluster"`
			CreatePartition bool `json:"create_partition"`
		} `json:"slurm_nodeset_workers"`
		Partitions []struct {
			Name             string   `json:"name"`
			IsAll            bool     `json:"is_all"`
			SlurmNodeSetRefs []string `json:"slurm_nodeset_refs"`
			Topology         string   `json:"topology"`
			Config           string   `json:"config"`
		} `json:"slurm_nodesets_partitions"`
	}
	require.NoError(t, json.Unmarshal(data, &rendered))
	require.Len(t, rendered.Workers, 3)
	var total int
	for i, worker := range rendered.Workers {
		ns := p.NodeSets[i]
		assert.Equal(t, ns.Name, worker.Name)
		assert.Equal(t, ns.Size, worker.Size)
		assert.Equal(t, ns.Platform, worker.Resource.Platform)
		assert.Equal(t, ns.Preset, worker.Resource.Preset)
		assert.True(t, worker.CreatePartition)
		total += worker.Size
		if ns.InfinibandFabric == "" {
			assert.Nil(t, worker.GPUCluster)
			continue
		}
		require.NotNil(t, worker.GPUCluster)
		assert.Equal(t, ns.InfinibandFabric, worker.GPUCluster.Fabric)
	}
	assert.Equal(t, 6, total)
	require.Len(t, rendered.Partitions, 4)
	assert.Equal(t, "main", rendered.Partitions[0].Name)
	assert.True(t, rendered.Partitions[0].IsAll)
	assert.Contains(t, rendered.Partitions[0].Config, "Default=YES")
	assert.Equal(t, "hidden", rendered.Partitions[1].Name)
	assert.True(t, rendered.Partitions[1].IsAll)
	assert.Contains(t, rendered.Partitions[1].Config, "Hidden=YES")
	for i, partition := range rendered.Partitions[2:] {
		assert.Equal(t, p.Partitions[i].Name, partition.Name)
		assert.Equal(t, p.Partitions[i].NodeSets, partition.SlurmNodeSetRefs)
		assert.False(t, partition.IsAll)
		assert.Equal(t, "flat", partition.Topology)
		assert.Contains(t, partition.Config, "Default=NO")
	}
}

func TestOverrideTestValuesDefaultPartitions(t *testing.T) {
	vars := overrideTestValues(map[string]interface{}{}, Config{Profile: validProfile()})
	partitions, ok := vars["slurm_nodesets_partitions"].([]interface{})
	require.True(t, ok)
	require.Len(t, partitions, 2)
	assert.Equal(t, "main", partitions[0].(map[string]interface{})["name"])
	assert.Equal(t, "hidden", partitions[1].(map[string]interface{})["name"])
}

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

func TestOverrideTestValuesUsesTestingActiveChecksScope(t *testing.T) {
	result := overrideTestValues(map[string]interface{}{}, Config{})
	assert.Equal(t, "testing", result["active_checks_scope"])
}

func TestOverrideTestValuesEnablesTailscale(t *testing.T) {
	result := overrideTestValues(map[string]interface{}{"tailscale_enabled": false}, Config{})
	assert.Equal(t, true, result["tailscale_enabled"])
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

func TestOverrideTestValuesLocalNVMe(t *testing.T) {
	const configYAML = `profiles:
  UK_B300:
    nebius_project_id: project-123
    nebius_region: uk-south1
    nebius_tenant_id: tenant-456
    nodesets:
      - name: worker
        platform: gpu-b300-sxm
        preset: 8gpu-192vcpu-2768gb
        size: 2
        infiniband_fabric: uk-south1-a
%s      - name: worker-cpu
        platform: cpu-d3
        preset: 32vcpu-128gb
        size: 2
`
	for _, tc := range []struct {
		name     string
		settings string
		wantJSON string
	}{
		{name: "omitted"},
		{
			name: "enabled with default mount",
			settings: `        local_nvme:
          enabled: true
          device_count: 6
          device_capacity_gigabytes: 3840
`,
			wantJSON: `{"enabled":true,"device_count":6,"device_capacity_gigabytes":3840}`,
		},
		{
			name: "shared release-4.1 config with custom mount",
			settings: `        local_nvme:
          enabled: true
          mount_path: /scratch
          filesystem_type: ext4
          device_count: 6
          device_capacity_gigabytes: 3840
`,
			wantJSON: `{"enabled":true,"mount_path":"/scratch","device_count":6,"device_capacity_gigabytes":3840}`,
		},
		{
			name: "explicitly disabled",
			settings: `        local_nvme:
          enabled: false
`,
			wantJSON: `{"enabled":false}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("E2E_CONFIG", fmt.Sprintf(configYAML, tc.settings))
			config, err := LoadE2EConfig()
			require.NoError(t, err)
			profile := config.Profiles["UK_B300"]
			raw, err := profile.YAML()
			require.NoError(t, err)
			profile, err = ParseProfile(raw)
			require.NoError(t, err)
			require.Equal(t, config.Profiles["UK_B300"], profile)

			vars := overrideTestValues(map[string]interface{}{}, Config{Profile: profile})
			data, err := json.Marshal(vars)
			require.NoError(t, err)
			var decoded struct {
				Workers []map[string]json.RawMessage `json:"slurm_nodeset_workers"`
			}
			require.NoError(t, json.Unmarshal(data, &decoded))
			require.Len(t, decoded.Workers, 2)
			gpu, cpu := decoded.Workers[0], decoded.Workers[1]
			assert.JSONEq(t, `"worker"`, string(gpu["name"]))
			assert.JSONEq(t, `"worker-cpu"`, string(cpu["name"]))
			if tc.wantJSON == "" {
				assert.NotContains(t, gpu, "local_nvme")
			} else {
				assert.JSONEq(t, tc.wantJSON, string(gpu["local_nvme"]))
			}
			assert.NotContains(t, cpu, "local_nvme")
			assert.JSONEq(t, `true`, string(gpu["create_partition"]))
			assert.JSONEq(t, `true`, string(cpu["create_partition"]))
		})
	}
}
