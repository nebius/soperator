package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"nebius.ai/slurm-operator/internal/e2e"
)

func TestWriteOutputsIncludesNebiusProfile(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "github-output")
	require.NoError(t, os.WriteFile(outputPath, nil, 0o600))
	t.Setenv("GITHUB_OUTPUT", outputPath)

	profile := e2e.Profile{
		NebiusProjectID: "project-testing",
		NebiusRegion:    "eu-north1",
		NebiusTenantID:  "tenant-testing",
		NebiusProfile:   "testing",
	}
	require.NoError(t, writeOutputs("MAN_GB300", profile, "profile: yaml"))

	contents, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "nebius_profile=testing\n")
}

func TestActivateProfileEnvironment(t *testing.T) {
	t.Setenv("NEBIUS_PROFILE", "production")
	t.Setenv("AWS_ENDPOINT_URL", "https://storage.production.example")
	require.NoError(t, activateProfileEnvironment(e2e.Profile{
		NebiusProfile:              "testing",
		TerraformBackendS3Endpoint: "https://storage.testing.example",
	}))
	assert.Equal(t, "testing", os.Getenv("NEBIUS_PROFILE"))
	assert.Equal(t, "https://storage.testing.example", os.Getenv("AWS_ENDPOINT_URL"))
}

func TestActivateProfileEnvironment_LegacyProfileKeepsEnvironment(t *testing.T) {
	t.Setenv("NEBIUS_PROFILE", "production")
	t.Setenv("AWS_ENDPOINT_URL", "https://storage.production.example")
	require.NoError(t, activateProfileEnvironment(e2e.Profile{}))
	assert.Equal(t, "production", os.Getenv("NEBIUS_PROFILE"))
	assert.Equal(t, "https://storage.production.example", os.Getenv("AWS_ENDPOINT_URL"))
}
