//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/kelseyhightower/envconfig"
	"github.com/stretchr/testify/require"
	ctyjson "github.com/zclconf/go-cty/cty/json"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type testConfig struct {
	PathToInstallation string   `split_words:"true" required:"true"`                // PATH_TO_INSTALLATION
	InfinibandFabric   string   `split_words:"true" required:"true"`                // INFINIBAND_FABRIC
	SSHKeys            []string `split_words:"true" required:"true"`                // SSH_KEYS
	O11yAccessToken    string   `split_words:"true" required:"true"`                // O11Y_ACCESS_TOKEN
	O11ySecretName     string   `split_words:"true" default:"o11y-writer-sa-token"` // O11Y_SECRET_NAME
	O11yNamespace      string   `split_words:"true" default:"logs-system"`          // O11Y_NAMESPACE
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
			"Context deadline exceeded": "retry on context deadline exceeded",
		},
		MaxRetries: 5,
	}

	terraform.Init(t, &commonOptions)
	terraform.WorkspaceSelectOrNew(t, &commonOptions, "e2e-test")
	terraform.Destroy(t, &commonOptions)

	defer terraform.Destroy(t, &commonOptions)

	// Set up resources in k8s that could not be setup in terraform
	targetedOptions := commonOptions
	targetedOptions.Targets = []string{"module.k8s"}
	terraform.Apply(t, &targetedOptions)

	applyO11ySecret(t, cfg)

	// Final apply
	terraform.Apply(t, &commonOptions)
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

	return tfVars
}

func applyO11ySecret(t *testing.T, cfg testConfig) {
	clientConfig, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedConfigDir)
	require.NoError(t, err)

	clientset, err := kubernetes.NewForConfig(clientConfig)
	require.NoError(t, err)

	_, err = clientset.CoreV1().Namespaces().Create(t.Context(), &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cfg.O11yNamespace,
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = clientset.CoreV1().Secrets("").Create(t.Context(), &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.O11ySecretName,
			Namespace: cfg.O11yNamespace,
		},
		StringData: map[string]string{
			"accessToken": cfg.O11yAccessToken,
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
}
