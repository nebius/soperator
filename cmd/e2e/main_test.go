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

func TestActivateNebiusProfile(t *testing.T) {
	t.Setenv("NEBIUS_PROFILE", "production")
	require.NoError(t, activateNebiusProfile(e2e.Profile{NebiusProfile: "testing"}))
	assert.Equal(t, "testing", os.Getenv("NEBIUS_PROFILE"))
}

func TestActivateNebiusProfile_LegacyProfileKeepsEnvironment(t *testing.T) {
	t.Setenv("NEBIUS_PROFILE", "production")
	require.NoError(t, activateNebiusProfile(e2e.Profile{}))
	assert.Equal(t, "production", os.Getenv("NEBIUS_PROFILE"))
}
