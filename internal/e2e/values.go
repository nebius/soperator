package e2e

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

func readTFVars(filename string) (map[string]interface{}, error) {
	tfVarsFile, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read tfvars file %s: %w", filename, err)
	}

	hclFile, diags := hclsyntax.ParseConfig(tfVarsFile, filename, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse HCL config %s: %w", filename, diags)
	}

	attrs, diags := hclFile.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("extract HCL attributes from %s: %w", filename, diags)
	}

	values := make(map[string]json.RawMessage, len(attrs))
	for name, attr := range attrs {
		value, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			return nil, fmt.Errorf("evaluate HCL attribute %q: %w", name, diags)
		}

		buf, err := ctyjson.Marshal(value, value.Type())
		if err != nil {
			return nil, fmt.Errorf("marshal HCL attribute %q to JSON: %w", name, err)
		}

		values[name] = json.RawMessage(buf)
	}

	output, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal values to JSON: %w", err)
	}

	var varsMap map[string]interface{}
	if err := json.Unmarshal(output, &varsMap); err != nil {
		return nil, fmt.Errorf("unmarshal values from JSON: %w", err)
	}

	return varsMap, nil
}

func overrideTestValues(tfVars map[string]interface{}, cfg Config) map[string]interface{} {
	tfVars["active_checks_scope"] = "testing"
	tfVars["slurm_operator_version"] = cfg.SoperatorVersion
	tfVars["slurm_operator_stable"] = !cfg.SoperatorUnstable
	tfVars["production"] = false
	tfVars["company_name"] = "e2e-test"

	tfVars["filestore_jail"] = map[string]interface{}{
		"spec": map[string]interface{}{
			"size_gibibytes":       2048,
			"block_size_kibibytes": 4,
		},
	}

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

	tfVars["slurm_nodeset_workers"] = []interface{}{
		map[string]interface{}{
			"name": "worker",
			"size": 2,
			"resource": map[string]interface{}{
				"platform": cfg.Profile.WorkerPlatform,
				"preset":   cfg.Profile.WorkerPreset,
			},
			"boot_disk": map[string]interface{}{
				"type":                 "NETWORK_SSD",
				"size_gibibytes":       2048,
				"block_size_kibibytes": 4,
			},
			"gpu_cluster": map[string]interface{}{
				"infiniband_fabric": cfg.Profile.InfinibandFabric,
			},
			"preemptible":      preemptibleValue(cfg.Profile.PreemptibleNodes),
			"features":         nil,
			"create_partition": nil,
		},
	}

	tfVars["slurm_login_ssh_root_public_keys"] = []string{cfg.SSHPublicKey}
	tfVars["etcd_cluster_size"] = 1
	tfVars["cleanup_bucket_on_destroy"] = true

	return tfVars
}

func preemptibleValue(enabled bool) interface{} {
	if enabled {
		return struct{}{}
	}
	return nil
}
