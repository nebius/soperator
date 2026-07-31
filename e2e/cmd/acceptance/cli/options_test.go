package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOptionsDefaults(t *testing.T) {
	opts, err := parseOptions([]string{"--kubectl-context", "dev-context"})
	require.NoError(t, err)

	assert.Equal(t, "dev-context", opts.KubectlContext)
	assert.Equal(t, "soperator", opts.SlurmClusterName)
	assert.False(t, opts.RunUnstableTests)
	assert.Empty(t, opts.SoperatorVersion)
	assert.Empty(t, opts.ScenarioPaths)
	assert.Empty(t, opts.ReportDir)
}

func TestParseOptionsExplicitValues(t *testing.T) {
	opts, err := parseOptions([]string{
		"--kubectl-context", "dev-context",
		"--slurm-cluster-name", "custom",
		"--soperator-version", "4.1.5-reb85d0e5",
		"--run-unstable=true",
		"--scenario", "features/internal_ssh.feature:3",
		"--scenario=features/topology.feature:3",
		"--report-dir", "reports",
	})
	require.NoError(t, err)

	assert.Equal(t, "dev-context", opts.KubectlContext)
	assert.Equal(t, "custom", opts.SlurmClusterName)
	assert.Equal(t, "4.1.5-reb85d0e5", opts.SoperatorVersion)
	assert.True(t, opts.RunUnstableTests)
	assert.Equal(t, []string{"features/internal_ssh.feature:3", "features/topology.feature:3"}, opts.ScenarioPaths)
	assert.Equal(t, "reports", opts.ReportDir)
}

func TestParseOptionsRequiresKubectlContext(t *testing.T) {
	_, err := parseOptions(nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "--kubectl-context is required")
}

func TestParseOptionsRejectsExtraArgs(t *testing.T) {
	_, err := parseOptions([]string{"--kubectl-context", "dev-context", "extra"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "unexpected acceptance arguments")
}

func TestParseOptionsRejectsEmptyScenario(t *testing.T) {
	_, err := parseOptions([]string{"--kubectl-context", "dev-context", "--scenario", " "})
	require.Error(t, err)
	assert.ErrorContains(t, err, "--scenario value cannot be empty")
}
