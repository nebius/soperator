//go:build e2e

package e2e_test

import (
	"context"
	"os/signal"
	"syscall"
	"testing"

	"github.com/kelseyhightower/envconfig"
	"github.com/stretchr/testify/require"
)

// TestTerraformApply runs terraform apply and validates the cluster
// This test does NOT destroy the cluster on completion to allow for debugging
func TestTerraformApply(t *testing.T) {
	var cfg testConfig

	err := envconfig.Process("", &cfg)
	require.NoError(t, err)

	runner := setupRunner(t, cfg)

	ensureOutputFiles(t, cfg)

	// Create context with signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	result, err := runner.Init(ctx)
	writeOutputs(t, cfg, "TestTerraformApply", "init", result.Combined(), err)
	require.NoError(t, err)

	result, err = runner.WorkspaceSelectOrNew(ctx, "e2e-test")
	writeOutputs(t, cfg, "TestTerraformApply", "workspace", result.Combined(), err)
	require.NoError(t, err)

	// Pre-test cleanup to ensure clean state
	result, err = runner.Destroy(ctx)
	writeOutputs(t, cfg, "TestTerraformApply", "destroy", result.Combined(), err)
	require.NoError(t, err)

	// Apply terraform configuration
	result, err = runner.Apply(ctx)
	writeOutputs(t, cfg, "TestTerraformApply", "apply", result.Combined(), err)
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

	runner := setupRunner(t, cfg)

	ensureOutputFiles(t, cfg)

	// Create context with signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	result, err := runner.Init(ctx)
	writeOutputs(t, cfg, "TestTerraformDestroy", "init", result.Combined(), err)
	require.NoError(t, err)

	result, err = runner.WorkspaceSelectOrNew(ctx, "e2e-test")
	writeOutputs(t, cfg, "TestTerraformDestroy", "workspace", result.Combined(), err)
	require.NoError(t, err)

	// Destroy the infrastructure
	result, err = runner.Destroy(ctx)
	writeOutputs(t, cfg, "TestTerraformDestroy", "destroy", result.Combined(), err)
	require.NoError(t, err)
}
