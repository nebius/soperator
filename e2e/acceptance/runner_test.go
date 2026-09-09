package acceptance

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const testTargetSoperatorVersion = "5.0.0"

func TestRunnerSuiteTagFilter(t *testing.T) {
	tests := []struct {
		name            string
		tags            string
		excludeUnstable bool
		want            string
	}{
		{
			name:            "default shared filters exclude unstable",
			excludeUnstable: true,
			want:            "~@unstable",
		},
		{
			name: "no filters when filters are disabled",
			want: "",
		},
		{
			name:            "custom suite tags are combined with option-driven filters",
			tags:            "@smoke && ~@slow",
			excludeUnstable: true,
			want:            "@smoke && ~@slow && ~@unstable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suite := testSampleSuite()
			suite.Tags = tt.tags
			suite.ExcludeUnstable = tt.excludeUnstable
			runner, err := NewRunner(RunnerConfig{
				KubectlContext:         "dev-context",
				TargetSoperatorVersion: testTargetSoperatorVersion,
				Suites:                 []SuiteConfig{suite},
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, runner.suiteTagFilter(runner.suites[0]))
		})
	}
}

func TestRunnerSuiteFeaturePaths(t *testing.T) {
	source := testFeatureSource()

	runner, err := NewRunner(RunnerConfig{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: "4.1.5-reb85d0e5",
		Suites: []SuiteConfig{
			{
				Name:        "sample",
				Source:      source,
				VersionAxes: []ScenarioVersionAxis{SoperatorVersionAxis("4.1.5-reb85d0e5")},
			},
		},
	})
	require.NoError(t, err)
	paths, err := runner.suiteFeaturePaths(&framework.ClusterInfo{TargetSoperatorVersion: "4.1.5"}, runner.suites[0])
	require.NoError(t, err)
	assert.Equal(t, []string{"features/sample.feature:3"}, paths)

	source.Paths = []string{"features/sample.feature:3", "features/sample.feature:7"}
	runner, err = NewRunner(RunnerConfig{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Suites: []SuiteConfig{
			{
				Name:        "sample",
				Source:      source,
				VersionAxes: []ScenarioVersionAxis{SoperatorVersionAxis(testTargetSoperatorVersion)},
			},
		},
	})
	require.NoError(t, err)
	paths, err = runner.suiteFeaturePaths(testClusterInfo(), runner.suites[0])
	require.NoError(t, err)
	assert.Equal(t, []string{"features/sample.feature:3", "features/sample.feature:7"}, paths)
}

func TestRunnerSuiteFeaturePathsAllowsUnversionedSuites(t *testing.T) {
	runner, err := NewRunner(RunnerConfig{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Suites: []SuiteConfig{
			{Name: "custom", Source: testFeatureSource()},
		},
	})
	require.NoError(t, err)

	paths, err := runner.suiteFeaturePaths(testClusterInfo(), runner.suites[0])
	require.NoError(t, err)
	assert.Equal(t, []string{"features/sample.feature"}, paths)
}

func TestNewRunnerSuiteValidation(t *testing.T) {
	tests := []struct {
		name   string
		suites []SuiteConfig
		want   string
	}{
		{
			name: "requires at least one suite",
			want: "at least one suite is required",
		},
		{
			name:   "requires suite name",
			suites: []SuiteConfig{{Source: testFeatureSource()}},
			want:   "suite name is required",
		},
		{
			name:   "requires filename-safe suite name",
			suites: []SuiteConfig{{Name: "sample suite", Source: testFeatureSource()}},
			want:   `suite name "sample suite" must match ^[A-Za-z0-9._-]+$`,
		},
		{
			name:   "rejects duplicate suite name",
			suites: []SuiteConfig{testSampleSuite(), testSampleSuite()},
			want:   `duplicate suite name "sample"`,
		},
		{
			name:   "requires source FS",
			suites: []SuiteConfig{{Name: "sample", Source: FeatureSource{Paths: []string{"features/sample.feature"}}}},
			want:   `suite "sample" source FS is required`,
		},
		{
			name:   "requires source paths",
			suites: []SuiteConfig{{Name: "sample", Source: FeatureSource{FS: fstest.MapFS{}}}},
			want:   `suite "sample" must include at least one feature path`,
		},
		{
			name: "rejects empty source path",
			suites: []SuiteConfig{
				{Name: "sample", Source: FeatureSource{FS: fstest.MapFS{}, Paths: []string{"features/sample.feature", " "}}},
			},
			want: `suite "sample" feature path cannot be empty`,
		},
		{
			name: "rejects nil step registrar",
			suites: []SuiteConfig{
				{
					Name:           "sample",
					Source:         testFeatureSource(),
					StepRegistrars: []StepRegistrar{nil},
				},
			},
			want: `suite "sample" step registrar 0 is nil`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRunner(RunnerConfig{
				KubectlContext:         "dev-context",
				TargetSoperatorVersion: testTargetSoperatorVersion,
				Suites:                 tt.suites,
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestNewRunnerRequiresTargetSoperatorVersion(t *testing.T) {
	_, err := NewRunner(RunnerConfig{
		KubectlContext: "dev-context",
		Suites:         []SuiteConfig{testSampleSuite()},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "target Soperator version is required")
}

func TestRunnerRunConfiguredSuitesRunsAllSuitesAndAggregatesFailures(t *testing.T) {
	var passingRuns int
	runner, err := NewRunner(RunnerConfig{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Suites: []SuiteConfig{
			{
				Name:           "failing",
				Source:         testRunnableFeatureSource("features/failing.feature", "a failing step runs"),
				StepRegistrars: []StepRegistrar{testRunnableStepRegistrar(&passingRuns)},
			},
			{
				Name:           "passing",
				Source:         testRunnableFeatureSource("features/passing.feature", "a passing step runs"),
				StepRegistrars: []StepRegistrar{testRunnableStepRegistrar(&passingRuns)},
			},
		},
	})
	require.NoError(t, err)

	err = runner.runConfiguredSuites(context.Background(), testClusterInfo(), nil, runner.suites)
	require.Error(t, err)
	assert.ErrorContains(t, err, `suite "failing": godog exited with status 1`)
	assert.Equal(t, 1, passingRuns)
}

func TestRunnerRunConfiguredSuitesSkipsEmptySuites(t *testing.T) {
	var passingRuns int
	runner, err := NewRunner(RunnerConfig{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Suites: []SuiteConfig{
			{
				Name:        "empty",
				Source:      testFeatureSource(),
				VersionAxes: []ScenarioVersionAxis{SoperatorVersionAxis("3.0.0")},
			},
			{
				Name:           "passing",
				Source:         testRunnableFeatureSource("features/passing.feature", "a passing step runs"),
				StepRegistrars: []StepRegistrar{testRunnableStepRegistrar(&passingRuns)},
			},
		},
	})
	require.NoError(t, err)

	require.NoError(t, runner.runConfiguredSuites(context.Background(), testClusterInfo(), nil, runner.suites))
	assert.Equal(t, 1, passingRuns)
}

func TestRunnerRunConfiguredSuitesErrorsWhenAllSuitesAreEmpty(t *testing.T) {
	runner, err := NewRunner(RunnerConfig{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Suites: []SuiteConfig{
			{
				Name:        "empty",
				Source:      testFeatureSource(),
				VersionAxes: []ScenarioVersionAxis{SoperatorVersionAxis("3.0.0")},
			},
		},
	})
	require.NoError(t, err)

	err = runner.runConfiguredSuites(context.Background(), testClusterInfo(), nil, runner.suites)
	require.Error(t, err)
	assert.ErrorContains(t, err, "no acceptance scenarios compatible with Soperator version 5.0.0")
}

func testSampleSuite() SuiteConfig {
	return SuiteConfig{
		Name:        "sample",
		Source:      testFeatureSource(),
		VersionAxes: []ScenarioVersionAxis{SoperatorVersionAxis(testTargetSoperatorVersion)},
	}
}

func testFeatureSource() FeatureSource {
	return FeatureSource{
		FS: fstest.MapFS{
			"features/sample.feature": {Data: []byte(`Feature: Sample
  @soperator_version_>=4.0.0
  Scenario: old scenario
    Then old behavior works

  @soperator_version_>=5.0.0
  Scenario: new scenario
    Then new behavior works
`)},
		},
		Paths: []string{"features/sample.feature"},
	}
}

func testRunnableFeatureSource(path, step string) FeatureSource {
	return FeatureSource{
		FS: fstest.MapFS{
			path: {Data: []byte(`Feature: Runnable
  Scenario: runs
    Then ` + step + `
`)},
		},
		Paths: []string{path},
	}
}

func testRunnableStepRegistrar(passingRuns *int) StepRegistrar {
	return func(sc *godog.ScenarioContext, info *framework.ClusterInfo, runtime framework.Runtime) {
		sc.Step(`^a passing step runs$`, func() error {
			*passingRuns = *passingRuns + 1
			return nil
		})
		sc.Step(`^a failing step runs$`, func() error {
			return errors.New("expected failure")
		})
	}
}

func testClusterInfo() *framework.ClusterInfo {
	return &framework.ClusterInfo{
		TargetSoperatorVersion: testTargetSoperatorVersion,
	}
}
