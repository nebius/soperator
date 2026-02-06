//go:build e2e

package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/kelseyhightower/envconfig"
	"github.com/stretchr/testify/require"
)

const k8sClusterName = "soperator-e2e-test"

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
	logTerraformState(t, &commonOptions)
	_, destroyErr := terraform.DestroyE(t, &commonOptions)
	if destroyErr != nil {
		t.Logf("Pre-cleanup destroy failed: %v", destroyErr)
		logTerraformState(t, &commonOptions)
		t.Fatalf("Pre-cleanup destroy failed, state may contain stuck resources: %v", destroyErr)
	}

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

	// Destroy the infrastructure with auto-recovery for unreachable K8s clusters
	logTerraformState(t, &commonOptions)
	destroyErr := destroyWithK8sRecovery(t, &commonOptions)
	require.NoError(t, destroyErr)
}

func logTerraformState(t *testing.T, opts *terraform.Options) {
	t.Helper()
	out, err := terraform.RunTerraformCommandE(t, opts, "state", "list")
	if err != nil {
		t.Logf("terraform state list failed: %v", err)
		return
	}
	if out == "" {
		t.Log("Terraform state is empty")
		return
	}
	t.Logf("Terraform state resources:\n%s", out)
}

func removeHelmReleasesFromState(t *testing.T, opts *terraform.Options) {
	t.Helper()
	out, err := terraform.RunTerraformCommandE(t, opts, "state", "list")
	if err != nil {
		t.Logf("terraform state list failed during helm release removal: %v", err)
		return
	}
	for _, resource := range strings.Split(out, "\n") {
		resource = strings.TrimSpace(resource)
		if resource == "" || !strings.Contains(resource, "helm_release") {
			continue
		}
		t.Logf("Removing %s from terraform state", resource)
		_, rmErr := terraform.RunTerraformCommandE(t, opts, "state", "rm", resource)
		if rmErr != nil {
			t.Logf("terraform state rm %s failed: %v", resource, rmErr)
		}
	}
}

func isMK8SClusterGone(t *testing.T, opts *terraform.Options) bool {
	t.Helper()
	projectID, ok := opts.EnvVars["NEBIUS_PROJECT_ID"]
	if !ok || projectID == "" {
		t.Log("NEBIUS_PROJECT_ID not set, cannot verify cluster existence")
		return false
	}
	out, err := exec.Command(
		"nebius", "mk8s", "cluster", "get-by-name",
		"--parent-id", projectID,
		"--name", k8sClusterName,
	).CombinedOutput()
	if err != nil {
		t.Logf("mk8s cluster %s not found (get-by-name failed: %v, output: %s)", k8sClusterName, err, string(out))
		return true
	}
	t.Logf("mk8s cluster %s still exists", k8sClusterName)
	return false
}

func destroyWithK8sRecovery(t *testing.T, opts *terraform.Options) error {
	t.Helper()
	_, err := terraform.DestroyE(t, opts)
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "Kubernetes cluster unreachable") {
		return err
	}
	if !isMK8SClusterGone(t, opts) {
		return err
	}
	t.Log("K8s cluster is confirmed gone, removing helm releases from state and retrying destroy")
	removeHelmReleasesFromState(t, opts)
	logTerraformState(t, opts)
	_, retryErr := terraform.DestroyE(t, opts)
	if retryErr != nil {
		return fmt.Errorf("destroy after helm release state cleanup: %w", retryErr)
	}
	return fmt.Errorf(
		"destroy recovered but K8s cluster %s was already gone, removed helm releases from state to unblock cleanup",
		k8sClusterName,
	)
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
