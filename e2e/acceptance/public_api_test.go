package acceptance_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"nebius.ai/soperator-e2e/acceptance"
	"nebius.ai/soperator-e2e/acceptance/framework"
	"nebius.ai/soperator-e2e/versionfilter"
)

func TestPublicRunnerAPIIsImportable(t *testing.T) {
	features := acceptance.SharedFeatureSource()

	require.NotNil(t, features.FS)
	assert.Contains(t, features.Paths, "features/internal_ssh.feature")

	suite := acceptance.SoperatorSuite("5.0.0")
	suite.Tags = "~@custom_only"

	runner, err := acceptance.NewRunner(acceptance.Options{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: "5.0.0",
		Suites:                 []acceptance.SuiteConfig{suite},
		State:                  &framework.ClusterState{},
	})
	require.NoError(t, err)
	assert.NotNil(t, runner)
}

func TestPublicRunnerAPIAcceptsExternalSuitesAndDiscoveryHooks(t *testing.T) {
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

	runner, err := acceptance.NewRunner(acceptance.Options{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: "5.0.0",
		Suites: []acceptance.SuiteConfig{
			acceptance.SoperatorSuite("5.0.0"),
			{
				Name:           "product",
				Source:         productFeatures,
				StepRegistrars: []acceptance.StepRegistrar{acceptance.SharedStepRegistrar()},
				VersionAxes: []versionfilter.Axis{
					acceptance.SoperatorVersionAxis("5.0.0"),
					{TagPrefix: "@product_version_", TargetVersion: "1.2.3"},
				},
			},
		},
		DiscoveryHooks: []acceptance.DiscoveryHook{
			func(ctx context.Context, state *framework.ClusterState, exec framework.Exec) error {
				state.Workers = append(state.Workers, framework.WorkerRef{Name: "custom-worker"})
				return nil
			},
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, runner)
}
