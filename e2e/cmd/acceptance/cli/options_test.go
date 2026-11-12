package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRunOptionsDefaults(t *testing.T) {
	opts, err := parseRunOptions([]string{"--kubectl-context", "dev-context", "--output-dir", "artifacts"})
	require.NoError(t, err)

	assert.Equal(t, "dev-context", opts.KubectlContext)
	assert.Equal(t, "soperator", opts.SlurmClusterName)
	assert.False(t, opts.RunUnstableTests)
	assert.False(t, opts.RunHeavyTests)
	assert.Empty(t, opts.SoperatorVersion)
	assert.Empty(t, opts.ScenarioPaths)
	assert.Equal(t, "artifacts", opts.OutputDir)
}

func TestParseRunOptionsExplicitValues(t *testing.T) {
	opts, err := parseRunOptions([]string{
		"--kubectl-context", "dev-context",
		"--slurm-cluster-name", "custom",
		"--soperator-version", "4.1.5-reb85d0e5",
		"--run-unstable=true",
		"--run-heavy=true",
		"--scenario", "features/internal_ssh.feature:3",
		"--scenario=features/observability.feature:3",
		"--output-dir", "reports",
	})
	require.NoError(t, err)

	assert.Equal(t, "custom", opts.SlurmClusterName)
	assert.True(t, opts.RunUnstableTests)
	assert.True(t, opts.RunHeavyTests)
	assert.Equal(t, []string{"features/internal_ssh.feature:3", "features/observability.feature:3"}, opts.ScenarioPaths)
	assert.Equal(t, "reports", opts.OutputDir)
}

func TestParseCollectOptionsProjectIsOptional(t *testing.T) {
	opts, err := parseCollectOptions([]string{"--kubectl-context", "dev-context", "--output-dir", "snapshot"})
	require.NoError(t, err)

	assert.Equal(t, "soperator", opts.SlurmClusterName)
	assert.Empty(t, opts.NebiusProjectID)
	assert.Equal(t, "snapshot", opts.OutputDir)
}

func TestParseCollectOptionsAllowsProjectWithoutKubernetesContext(t *testing.T) {
	opts, err := parseCollectOptions([]string{"--nebius-project-id", "project-id", "--output-dir", "snapshot"})
	require.NoError(t, err)

	assert.Empty(t, opts.KubectlContext)
	assert.Equal(t, "project-id", opts.NebiusProjectID)
}

func TestParseCollectOptionsRequiresAtLeastOneArtifactSource(t *testing.T) {
	_, err := parseCollectOptions([]string{"--output-dir", "snapshot"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "at least one of --kubectl-context or --nebius-project-id is required")
}

func TestSuiteFromOptionsFiltersHeavyAndUnstableIndependently(t *testing.T) {
	for _, heavy := range []bool{false, true} {
		for _, unstable := range []bool{false, true} {
			suite := suiteFromOptions(runOptions{RunHeavyTests: heavy, RunUnstableTests: unstable}, "5.0.0")
			assert.Empty(t, suite.Tags)
			assert.Equal(t, !heavy, suite.ExcludeHeavy)
			assert.Equal(t, !unstable, suite.ExcludeUnstable)
		}
	}
}

func TestSuiteFromOptionsRunsManualScenariosOnlyWithSelectors(t *testing.T) {
	assert.True(t, suiteFromOptions(runOptions{}, "5.0.0").ExcludeManual)
	assert.True(t, suiteFromOptions(runOptions{RunHeavyTests: true, RunUnstableTests: true}, "5.0.0").ExcludeManual)
	assert.False(t, suiteFromOptions(runOptions{ScenarioPaths: []string{"features/internal_ssh.feature:3"}}, "5.0.0").ExcludeManual)
}

func TestParseRunOptionsRequiresCommonOptions(t *testing.T) {
	_, err := parseRunOptions(nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "--kubectl-context is required")

	_, err = parseRunOptions([]string{"--kubectl-context", "dev-context"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "--output-dir is required")
}

func TestParseRunOptionsRejectsExtraArgs(t *testing.T) {
	_, err := parseRunOptions([]string{"--kubectl-context", "dev-context", "--output-dir", "out", "extra"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "unexpected run arguments")
}

func TestParseRunOptionsRejectsEmptyScenario(t *testing.T) {
	_, err := parseRunOptions([]string{"--kubectl-context", "dev-context", "--output-dir", "out", "--scenario", " "})
	require.Error(t, err)
	assert.ErrorContains(t, err, "--scenario value cannot be empty")
}

func TestRunRequiresExplicitSubcommand(t *testing.T) {
	err := Run(context.Background(), nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "subcommand is required")
}

func TestRunResolvesScenarioAliasesBeforeVersionFiltering(t *testing.T) {
	for _, selector := range []string{
		"features/worker_rolling_update.feature:14",
		"e2e/acceptance/features/worker_rolling_update.feature:14",
		"/checkout/soperator/e2e/acceptance/features/worker_rolling_update.feature:14",
	} {
		t.Run(selector, func(t *testing.T) {
			err := Run(context.Background(), []string{
				"run", "--kubectl-context", "unused-alias-test-context",
				"--soperator-version", "4.1.14", "--output-dir", t.TempDir(),
				"--scenario", selector, "--run-heavy",
			})
			require.ErrorContains(t, err, "no acceptance scenarios compatible with Soperator version 4.1.14")
		})
	}
}
