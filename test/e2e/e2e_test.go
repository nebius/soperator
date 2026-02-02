//go:build e2e

package e2e_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/kelseyhightower/envconfig"
	"github.com/stretchr/testify/require"
)

// TestTerraformApply runs terraform apply and validates the cluster
// This test does NOT destroy the cluster on completion to allow for debugging
func TestTerraformApply(t *testing.T) {
	var cfg testConfig

	err := envconfig.Process("", &cfg)
	require.NoError(t, err)

	commonOptions := setupTerraformOptions(t, cfg)

	terraform.Init(t, &commonOptions)
	terraform.WorkspaceSelectOrNew(t, &commonOptions, "e2e-test")

	// Pre-test cleanup to ensure clean state
	terraform.Destroy(t, &commonOptions)

	// Apply terraform configuration
	terraform.Apply(t, &commonOptions)

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

	terraform.Init(t, &commonOptions)
	terraform.WorkspaceSelectOrNew(t, &commonOptions, "e2e-test")

	// Destroy the infrastructure
	terraform.Destroy(t, &commonOptions)
}

func setupTerraformOptions(t *testing.T, cfg testConfig) terraform.Options {
	tfVars := readTFVars(t, fmt.Sprintf("%s/terraform.tfvars", cfg.PathToInstallation))
	tfVars = overrideTestValues(t, tfVars, cfg)

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
		NoColor:      true,
	}
}
