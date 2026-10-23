package e2e

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				"enabled": tc.enabled, "version": "1.2.0", "use_stable_repo": true,
				"size_gibibytes": 3720, "threads": 32, "disk_type": "NETWORK_SSD_IO_M3",
			}
			result := overrideTestValues(map[string]interface{}{"nfs_in_k8s": nfs}, Config{
				SoperatorVersion: "4.1.12-r2012dad5", SoperatorUnstable: tc.unstable, NFSVersion: tc.version,
			})
			assert.Equal(t, map[string]interface{}{
				"enabled": tc.enabled, "version": tc.version, "use_stable_repo": !tc.unstable,
				"size_gibibytes": 3720, "threads": 32, "disk_type": "NETWORK_SSD_IO_M3",
			}, result["nfs_in_k8s"])
		})
	}
}

func TestOverrideTestValuesWithoutNFS(t *testing.T) {
	result := overrideTestValues(map[string]interface{}{}, Config{NFSVersion: "1.2.2"})
	assert.NotContains(t, result, "nfs_in_k8s")
	result = overrideTestValues(map[string]interface{}{"nfs_in_k8s": nil}, Config{NFSVersion: "1.2.2"})
	assert.Nil(t, result["nfs_in_k8s"])
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
			name: "enabled with Terraform defaults",
			settings: `        local_nvme:
          enabled: true
`,
			wantJSON: `{"enabled":true}`,
		},
		{
			name: "custom mount and filesystem",
			settings: `        local_nvme:
          enabled: true
          mount_path: /scratch
          filesystem_type: xfs
`,
			wantJSON: `{"enabled":true,"mount_path":"/scratch","filesystem_type":"xfs"}`,
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
