//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/require"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

func readTFVars(t *testing.T, tfVarsFilename string) map[string]interface{} {
	tfVarsFile, err := os.ReadFile(tfVarsFilename)
	require.NoError(t, err)

	hclFile, diags := hclsyntax.ParseConfig(tfVarsFile, tfVarsFilename, hcl.InitialPos)
	require.False(t, diags.HasErrors(), diags.Error())
	require.NotNil(t, hclFile)

	attrs, diags := hclFile.Body.JustAttributes()
	require.False(t, diags.HasErrors(), diags.Error())

	values := make(map[string]json.RawMessage, len(attrs))
	for name, attr := range attrs {
		value, diags := attr.Expr.Value(nil)
		require.False(t, diags.HasErrors(), diags.Error())

		buf, err := ctyjson.Marshal(value, value.Type())
		require.NoError(t, err)

		values[name] = json.RawMessage(buf)
	}

	output, err := json.MarshalIndent(values, "", "  ")
	require.NoError(t, err)

	var varsMap map[string]interface{}

	err = json.Unmarshal(output, &varsMap)
	require.NoError(t, err)

	return varsMap
}

func overrideTestValues(t *testing.T, tfVars map[string]interface{}, cfg testConfig) map[string]interface{} {
	// active_checks_scope = "prod"
	tfVars["active_checks_scope"] = "testing"

	// slurm_operator_version = "1.19.0"
	tfVars["slurm_operator_version"] = cfg.SoperatorVersion
	// slurm_operator_stable = true
	tfVars["slurm_operator_stable"] = !cfg.SoperatorUnstable
	// production = true
	tfVars["production"] = false

	// company_name = "e2e-test"
	tfVars["company_name"] = "e2e-test"

	// nfs_in_k8s = {
	//    enabled         = true
	//    version         = "1.2.0-f67979d7"
	//    size_gibibytes  = 3720
	//    disk_type       = "NETWORK_SSD_IO_M3"
	//    filesystem_type = "ext4"
	// }
	tfVars["nfs_in_k8s"] = map[string]interface{}{
		"enabled":         true,
		"version":         "1.2.0-f67979d7",
		"size_gibibytes":  3720,
		"disk_type":       "NETWORK_SSD_IO_M3",
		"filesystem_type": "ext4",
	}

	// filestore_jail = {
	//   spec = {
	//     size_gibibytes       = 2048
	//     block_size_kibibytes = 4
	//   }
	// }
	tfVars["filestore_jail"] = map[string]interface{}{
		"spec": map[string]interface{}{
			"size_gibibytes":       2048,
			"block_size_kibibytes": 4,
		},
	}

	//  filestore_jail_submounts = [{
	//    name       = "data"
	//    mount_path = "/mnt/data"
	//    spec = {
	//    size_gibibytes       = 2048
	//    block_size_kibibytes = 4
	//    }
	//  }]
	tfVars["filestore_jail_submounts"] = []interface{}{
		map[string]interface{}{
			"name":       "data",
			"mount_path": "/mnt/data",
			"spec": map[string]interface{}{
				"size_gibibytes":       2048,
				"block_size_kibibytes": 4,
			},
		},
	}

	// slurm_nodesets_enabled = false
	tfVars["slurm_nodesets_enabled"] = true

	// slurm_nodeset_workers = [
	//   {
	//     name = "worker"
	//     size = 128
	//     resource = {
	//       platform = "gpu-h100-sxm"
	//       preset   = "8gpu-128vcpu-1600gb"
	//     }
	//     boot_disk = {
	//       type                 = "NETWORK_SSD"
	//       size_gibibytes       = 512
	//       block_size_kibibytes = 4
	//     }
	//     gpu_cluster = {
	//       infiniband_fabric = ""
	//     }
	//     # Change to preemptible = {} in case you want to use preemptible nodes
	//     preemptible = null
	//     # Provide a list of strings to set Slurm Node features
	//     features = null
	//     # Set to `true` to create partition for the NodeSet by default
	//     create_partition = null
	//   },
	// ]
	tfVars["slurm_nodeset_workers"] = []interface{}{
		map[string]interface{}{
			"name": "worker",
			"size": 2,
			"resource": map[string]interface{}{
				"platform": cfg.WorkerPlatform,
				"preset":   cfg.WorkerPreset,
			},
			"boot_disk": map[string]interface{}{
				"type":                 "NETWORK_SSD",
				"size_gibibytes":       2048,
				"block_size_kibibytes": 4,
			},
			"gpu_cluster": map[string]interface{}{
				"infiniband_fabric": cfg.InfinibandFabric,
			},
			// User regular nodes for now
			// "preemptible": struct{}{},
			"preemptible":      nil,
			"features":         nil,
			"create_partition": nil,
		},
	}

	// slurm_nodesets_partitions = [
	//   {
	//     name   = "workers"
	//     is_all = false
	//     config = "Default=NO PriorityTier=10 MaxTime=INFINITE State=UP OverSubscribe=YES"
	//     nodeset_refs = [
	//       "worker",
	//     ]
	//   },
	// ]
	tfVars["slurm_nodesets_partitions"] = []any{
		map[string]any{
			"name":   "workers",
			"is_all": false,
			"config": strings.TrimSpace(fmt.Sprintf(
				"Default=YES PriorityTier=10 MaxTime=INFINITE State=UP OverSubscribe=YES %s",
				renderDefCpuPerGpu(t, cfg),
			)),
			"nodeset_refs": []string{
				"worker",
			},
		},
	}

	// slurm_login_ssh_root_public_keys = [
	//   "ssh-rsa somekey==",
	// ]
	tfVars["slurm_login_ssh_root_public_keys"] = cfg.SSHKeys

	// Not HA, so it'll k8s cluster will be created faster
	tfVars["etcd_cluster_size"] = 1

	// cleanup_bucket_on_destroy = false
	tfVars["cleanup_bucket_on_destroy"] = true

	return tfVars
}

func renderDefCpuPerGpu(t *testing.T, cfg testConfig) string {
	if !strings.HasPrefix(cfg.WorkerPlatform, "gpu") {
		return ""
	}

	presetComponents := strings.Split(cfg.WorkerPreset, "-")
	require.GreaterOrEqual(t, len(presetComponents), 3,
		"gpu worker preset must contain at least gpu, cpu, and memory specifiers",
	)

	var (
		gpusString, cpusString string
	)
	for _, component := range presetComponents {
		if strings.HasSuffix(component, "gpu") {
			gpusString = strings.TrimSuffix(component, "gpu")
			continue
		}
		if strings.HasSuffix(component, "vcpu") {
			cpusString = strings.TrimSuffix(component, "vcpu")
			continue
		}
	}
	require.NotEmpty(t, gpusString, "worker preset must have gpu specifier")
	require.NotEmpty(t, cpusString, "worker preset must have vcpu specifier")

	var (
		gpus, cpus int
		err        error
	)
	gpus, err = strconv.Atoi(gpusString)
	require.NoError(t, err, "failed to parse gpu count from preset specifier: %q", gpusString)
	require.Greater(t, gpus, 0, "gpu count must be greater than zero")
	cpus, err = strconv.Atoi(cpusString)
	require.NoError(t, err, "failed to parse cpu count from preset specifier: %q", cpusString)
	require.Greater(t, cpus, 0, "cpu count must be greater than zero")

	var cpuPerGpu int = cpus / gpus
	require.Greater(t, cpuPerGpu, 0, "cpu per gpu must be greater than zero")

	return fmt.Sprintf("DefCpuPerGPU=%d", cpuPerGpu)
}
