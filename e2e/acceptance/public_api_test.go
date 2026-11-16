package acceptance_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance"
)

func TestPublicRunnerAPIIsImportable(t *testing.T) {
	features := acceptance.SharedFeatureSource()

	require.NotNil(t, features.FS)
	assert.Contains(t, features.Paths, "features/internal_ssh.feature")

	suite := acceptance.SoperatorSuite("5.0.0")
	suite.Tags = "~@custom_only"
	assert.False(t, suite.ExcludeHeavy, "library callers retain heavy coverage by default")
	assert.True(t, suite.ExcludeManual, "library callers must name @manual scenarios explicitly")

	runner, err := acceptance.NewRunner(acceptance.RunnerConfig{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: "5.0.0",
		Suites:                 []acceptance.SuiteConfig{suite},
	})
	require.NoError(t, err)
	assert.NotNil(t, runner)
}

func TestPublicRuntimeAPIIsImportable(t *testing.T) {
	runtime := acceptance.NewRuntime("dev-context", "soperator")
	assert.NotNil(t, runtime)
}

func TestNormalizeScenarioPaths(t *testing.T) {
	for _, alias := range []string{
		"features/active_checks.feature",
		"e2e/acceptance/features/active_checks.feature",
		"./e2e/acceptance/features/active_checks.feature",
		"acceptance/features/active_checks.feature",
		"/checkout/soperator/e2e/acceptance/features/active_checks.feature",
	} {
		for _, suffix := range []string{"", ":3"} {
			got, err := acceptance.NormalizeScenarioPaths([]string{alias + suffix})
			require.NoError(t, err)
			assert.Equal(t, []string{"features/active_checks.feature" + suffix}, got)
		}
	}
	for _, invalid := range []string{"", "features/missing.feature", "features/active_checks.feature:0", "features/active_checks.feature:abc"} {
		_, err := acceptance.NormalizeScenarioPaths([]string{invalid})
		require.Error(t, err)
	}
	paths, err := acceptance.NormalizeScenarioPaths([]string{
		"e2e/acceptance/features/active_checks.feature:3", "features/active_checks.feature:3",
	})
	assert.NoError(t, err)
	assert.Equal(t, paths[0], paths[1])
}

func TestPublicRunnerAPIAcceptsCallerOwnedStaticSuites(t *testing.T) {
	productFeatures := acceptance.FeatureSource{
		FS: fstest.MapFS{
			"features/product.feature": {Data: []byte(`Feature: Product
  @product_version_>=1.0.0
  @soperator_version_>=5.0.0
  Scenario: custom scenario
    Then custom behavior works
`)},
		},
		Paths: []string{"features/product.feature"},
	}

	runner, err := acceptance.NewRunner(acceptance.RunnerConfig{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: "5.0.0",
		Suites: []acceptance.SuiteConfig{
			acceptance.SoperatorSuite("5.0.0"),
			{
				Name:           "product",
				Source:         productFeatures,
				StepRegistrars: []acceptance.StepRegistrar{acceptance.SharedStepRegistrar()},
				VersionAxes: []acceptance.ScenarioVersionAxis{
					acceptance.SoperatorVersionAxis("5.0.0"),
					acceptance.VersionAxis("@product_version_", "1.2.3"),
				},
			},
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, runner)
}
