//go:build e2e

package e2e_test

import (
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
