//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	SSHKeys            []string `split_words:"true" required:"true"`                // SSH_KEYS
	O11yAccessToken    string   `split_words:"true" required:"true"`                // O11Y_ACCESS_TOKEN
	O11ySecretName     string   `split_words:"true" default:"o11y-writer-sa-token"` // O11Y_SECRET_NAME
	O11yNamespace      string   `split_words:"true" default:"logs-system"`          // O11Y_NAMESPACE
	OutputLogFile      string   `split_words:"true" default:"output.log"`           // OUTPUT_LOG_FILE
	OutputErrFile      string   `split_words:"true" default:"output.err"`           // OUTPUT_ERR_FILE
}

func TestTerraform(t *testing.T) {
	var cfg testConfig

	err := envconfig.Process("", &cfg)
	require.NoError(t, err)

	tfVars := readTFVars(t, fmt.Sprintf("%s/terraform.tfvars", cfg.PathToInstallation))
	tfVars = overrideTestValues(tfVars, cfg)

	envVarsList := os.Environ()
	envVars := make(map[string]string)
	for _, envVar := range envVarsList {
		pair := strings.SplitN(envVar, "=", 2)
		require.Len(t, pair, 2)
		envVars[pair[0]] = pair[1]
	}

	commonOptions := terraform.Options{
		TerraformDir: cfg.PathToInstallation,
		Vars:         tfVars,
		EnvVars:      envVars,
		RetryableTerraformErrors: map[string]string{
			"(?m)^.*context deadline exceeded.*$":  "retry on context deadline exceeded",
			"(?m)^.*connection reset by peer.*$":   "retry on conn reset by peer",
			"(?m)^.*etcdserver: leader changed.*$": "retry on leader changed",
		},
		NoColor:    true,
		MaxRetries: 5,
	}

	ensureOutputFiles(t, cfg)

	terraform.Init(t, &commonOptions)
	terraform.WorkspaceSelectOrNew(t, &commonOptions, "e2e-test")
	output, err := terraform.DestroyE(t, &commonOptions)
	writeOutputs(t, cfg, "destroy", output, err)
	require.NoError(t, err)

	defer func() {
		output, err := terraform.DestroyE(t, &commonOptions)
		writeOutputs(t, cfg, "destroy", output, err)
		require.NoError(t, err)
	}()

	output, err = terraform.ApplyE(t, &commonOptions)
	writeOutputs(t, cfg, "apply", output, err)
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
	// slurm_operator_version = "1.19.0"
	tfVars["slurm_operator_version"] = cfg.SoperatorVersion
	// slurm_operator_stable = true
	tfVars["slurm_operator_stable"] = !cfg.SoperatorUnstable

	// company_name = "e2e-test"
	tfVars["company_name"] = "e2e-test"

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

	// slurm_nodeset_workers = [{
	// 	 size                    = 2
	// 	 nodes_per_nodegroup     = 1
	// 	 max_unavailable_percent = 50
	// 	 resource = {
	// 	   platform = "gpu-h100-sxm"
	// 	   preset   = "8gpu-128vcpu-1600gb"
	// 	 }
	// 	 boot_disk = {
	// 	   type                 = "NETWORK_SSD"
	// 	   size_gibibytes       = 2048
	// 	   block_size_kibibytes = 4
	// 	 }
	// 	 gpu_cluster = {
	// 	   infiniband_fabric = ""
	// 	 }
	// }]
	tfVars["slurm_nodeset_workers"] = []interface{}{
		map[string]interface{}{
			"size":                    2,
			"nodes_per_nodegroup":     1,
			"max_unavailable_percent": 50,
			"resource": map[string]interface{}{
				"platform": "gpu-h100-sxm",
				"preset":   "8gpu-128vcpu-1600gb",
			},
			"boot_disk": map[string]interface{}{
				"type":                 "NETWORK_SSD",
				"size_gibibytes":       2048,
				"block_size_kibibytes": 4,
			},
			"gpu_cluster": map[string]interface{}{
				"infiniband_fabric": cfg.InfinibandFabric,
			},
		},
	}

	// slurm_login_ssh_root_public_keys = [
	//   "ssh-rsa somekey==",
	// ]
	tfVars["slurm_login_ssh_root_public_keys"] = cfg.SSHKeys

	// github_branch = "main"
	tfVars["github_branch"] = "dev"

	return tfVars
}

func ensureOutputFiles(t *testing.T, cfg testConfig) {
	ensureFile(t, cfg.OutputLogFile)
	ensureFile(t, cfg.OutputErrFile)
}

func ensureFile(t *testing.T, filename string) {
	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0755))

	f, err := os.Create(filename)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func writeOutputs(t *testing.T, cfg testConfig, command, output string, err error) {
	writeOutput(t, cfg.OutputLogFile, command, output)
	if err != nil {
		writeOutput(t, cfg.OutputErrFile, command, err.Error())
	}
}

func writeOutput(t *testing.T, filename, command, data string) {
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer f.Close()

	_, err = f.WriteString(fmt.Sprintf("Executing %s\n\n", command))
	require.NoError(t, err)

	_, err = f.WriteString(fmt.Sprintf("%s\n\n", data))
	require.NoError(t, err)
}
