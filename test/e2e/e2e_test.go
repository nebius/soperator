//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/kelseyhightower/envconfig"
	"github.com/stretchr/testify/require"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

type testConfig struct {
	SoperatorVersion   string   `split_words:"true" required:"true"`                // SOPERATOR_VERSION
	SoperatorUnstable  bool     `split_words:"true" required:"true"`                // SOPERATOR_UNSTABLE
	PathToInstallation string   `split_words:"true" required:"true"`                // PATH_TO_INSTALLATION
	InfinibandFabric   string   `split_words:"true" required:"true"`                // INFINIBAND_FABRIC
	WorkerPlatform     string   `split_words:"true" required:"true"`                // WORKER_PLATFORM
	WorkerPreset       string   `split_words:"true" required:"true"`                // WORKER_PRESET
	SSHKeys            []string `split_words:"true" required:"true"`                // SSH_KEYS
	O11yAccessToken    string   `split_words:"true" required:"true"`                // O11Y_ACCESS_TOKEN
	O11ySecretName     string   `split_words:"true" default:"o11y-writer-sa-token"` // O11Y_SECRET_NAME
	O11yNamespace      string   `split_words:"true" default:"logs-system"`          // O11Y_NAMESPACE
	OutputLogFile      string   `split_words:"true" default:"output.log"`           // OUTPUT_LOG_FILE
	OutputErrFile      string   `split_words:"true" default:"output.err"`           // OUTPUT_ERR_FILE
}

// setupTerraformOptions creates common terraform options for e2e tests
func setupTerraformOptions(t *testing.T, cfg testConfig) terraform.Options {
	tfVars := readTFVars(t, fmt.Sprintf("%s/terraform.tfvars", cfg.PathToInstallation))
	tfVars = overrideTestValues(tfVars, cfg)

	envVarsList := os.Environ()
	envVars := make(map[string]string)
	for _, envVar := range envVarsList {
		pair := strings.SplitN(envVar, "=", 2)
		require.Len(t, pair, 2)
		envVars[pair[0]] = pair[1]
	}

	return terraform.Options{
		TerraformDir: cfg.PathToInstallation,
		Vars:         tfVars,
		EnvVars:      envVars,
		RetryableTerraformErrors: map[string]string{
			"(?m)^.*context deadline exceeded.*$":  "retry on context deadline exceeded",
			"(?m)^.*connection reset by peer.*$":   "retry on conn reset by peer",
			"(?m)^.*etcdserver: leader changed.*$": "retry on leader changed",
			"(?m)^.*resource deletion failed.*$":   "retry on allocation delete",
		},
		NoColor:    true,
		MaxRetries: 5,
	}
}

// TestTerraformApply runs terraform apply and validates the cluster
// This test does NOT destroy the cluster on completion to allow for debugging
func TestTerraformApply(t *testing.T) {
	var cfg testConfig

	err := envconfig.Process("", &cfg)
	require.NoError(t, err)

	commonOptions := setupTerraformOptions(t, cfg)

	ensureOutputFiles(t, cfg)

	terraform.Init(t, &commonOptions)
	terraform.WorkspaceSelectOrNew(t, &commonOptions, "e2e-test")

	// Pre-test cleanup to ensure clean state
	output, err := terraform.DestroyE(t, &commonOptions)
	writeOutputs(t, cfg, "TestTerraformApply", "destroy", output, err)
	require.NoError(t, err)

	// Apply terraform configuration
	output, err = terraform.ApplyE(t, &commonOptions)
	writeOutputs(t, cfg, "TestTerraformApply", "apply", output, err)
	require.NoError(t, err)

	// Note: No defer destroy - cleanup is handled by TestTerraformDestroy
	// This allows cluster state collection on failure in CI
}

// TestTerraformDestroy cleans up the infrastructure created by TestTerraformApply
// This is run as a separate test to allow cluster state collection on failure
func TestTerraformDestroy(t *testing.T) {
	var cfg testConfig

	err := envconfig.Process("", &cfg)
	require.NoError(t, err)

	commonOptions := setupTerraformOptions(t, cfg)

	ensureOutputFiles(t, cfg)

	terraform.Init(t, &commonOptions)
	terraform.WorkspaceSelectOrNew(t, &commonOptions, "e2e-test")

	// Destroy the infrastructure
	output, err := terraform.DestroyE(t, &commonOptions)
	writeOutputs(t, cfg, "TestTerraformDestroy", "destroy", output, err)
	require.NoError(t, err)
}

func readTFVars(t *testing.T, tfVarsFilename string) map[string]interface{} {
	tfVarsFile, err := os.ReadFile(tfVarsFilename)
	require.NoError(t, err)

	hclFile, diags := hclsyntax.ParseConfig(tfVarsFile, tfVarsFilename, hcl.InitialPos)
	require.False(t, diags.HasErrors(), diags.Error())

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

func overrideTestValues(tfVars map[string]interface{}, cfg testConfig) map[string]interface{} {
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

func ensureOutputFiles(t *testing.T, cfg testConfig) {
	ensureFile(t, cfg.OutputLogFile)
	ensureFile(t, cfg.OutputErrFile)
}

func ensureFile(t *testing.T, filename string) {
	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0755))

	// Use O_CREATE without O_TRUNC to create file if not exists, but don't truncate if exists
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func writeOutputs(t *testing.T, cfg testConfig, testPhase, command, output string, err error) {
	writeOutput(t, cfg.OutputLogFile, testPhase, command, output)
	if err != nil {
		writeOutput(t, cfg.OutputErrFile, testPhase, command, err.Error())
	}
}

func writeOutput(t *testing.T, filename, testPhase, command, data string) {
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer f.Close()

	// Write phase header with timestamp
	_, err = f.WriteString(fmt.Sprintf("========================================\n"))
	require.NoError(t, err)
	_, err = f.WriteString(fmt.Sprintf("Test Phase: %s\n", testPhase))
	require.NoError(t, err)
	_, err = f.WriteString(fmt.Sprintf("Timestamp: %s\n", time.Now().Format("2006-01-02 15:04:05 MST")))
	require.NoError(t, err)
	_, err = f.WriteString(fmt.Sprintf("========================================\n\n"))
	require.NoError(t, err)

	_, err = f.WriteString(fmt.Sprintf("Executing %s\n\n", command))
	require.NoError(t, err)

	_, err = f.WriteString(fmt.Sprintf("%s\n\n", data))
	require.NoError(t, err)
}
