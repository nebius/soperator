package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverrideTerraformBackendS3Endpoint(t *testing.T) {
	workdir := t.TempDir()
	path := filepath.Join(workdir, terraformBackendOverrideFile)
	require.NoError(t, os.WriteFile(path, []byte(`terraform {
  backend "s3" {
    endpoints = {
      s3 = "https://storage.eu-north1.nebius.cloud:443"
    }
  }
}
`), 0o600))

	require.NoError(t, overrideTerraformBackendS3Endpoint(workdir, "https://storage.testing.example"))

	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(contents), `s3 = "https://storage.testing.example"`)
	assert.NotContains(t, string(contents), "storage.eu-north1.nebius.cloud")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestOverrideTerraformBackendS3Endpoint_AbsentOverrideIsNoOp(t *testing.T) {
	require.NoError(t, overrideTerraformBackendS3Endpoint(t.TempDir(), ""))
}

func TestOverrideTerraformBackendS3Endpoint_RequiresOneEndpoint(t *testing.T) {
	workdir := t.TempDir()
	path := filepath.Join(workdir, terraformBackendOverrideFile)
	require.NoError(t, os.WriteFile(path, []byte("terraform {}\n"), 0o600))

	err := overrideTerraformBackendS3Endpoint(workdir, "https://storage.testing.example")
	assert.ErrorContains(t, err, "found 0")
}
