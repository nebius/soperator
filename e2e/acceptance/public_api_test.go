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

	runner, err := acceptance.NewRunner(acceptance.RunnerConfig{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: "5.0.0",
		Suites:                 []acceptance.SuiteConfig{suite},
	})
	require.NoError(t, err)
	assert.NotNil(t, runner)
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
