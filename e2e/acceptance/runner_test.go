package acceptance

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"nebius.ai/soperator-e2e/acceptance/framework"
	"nebius.ai/soperator-e2e/versionfilter"
)

const testTargetSoperatorVersion = "5.0.0"

func TestRunnerSuiteTagFilter(t *testing.T) {
	cpuAndGPUState := &framework.ClusterState{
		CPUWorkers: []framework.WorkerRef{{Name: "worker-cpu-0"}},
		GPUWorkers: []framework.WorkerRef{{Name: "worker-gpu-0"}},
	}
	noGPUState := &framework.ClusterState{}
	gpuOnlyState := &framework.ClusterState{
		GPUWorkers: []framework.WorkerRef{{Name: "worker-gpu-0"}},
	}

	tests := []struct {
		name          string
		state         *framework.ClusterState
		tags          string
		filterOptions SuiteFilterOptions
		want          string
	}{
		{
			name:  "default shared filters exclude unstable",
			state: cpuAndGPUState,
			filterOptions: SuiteFilterOptions{
				ExcludeUnstable:           true,
				ExcludeMissingWorkerKinds: true,
			},
			want: "~@unstable",
		},
		{
			name:          "no filters when CPU and GPU workers exist and filters are disabled",
			state:         cpuAndGPUState,
			filterOptions: SuiteFilterOptions{},
			want:          "",
		},
		{
			name:  "without workers also excludes GPU and CPU",
			state: noGPUState,
			filterOptions: SuiteFilterOptions{
				ExcludeUnstable:           true,
				ExcludeMissingWorkerKinds: true,
			},
			want: "~@unstable && ~@gpu && ~@cpu",
		},
		{
			name:  "without CPU workers excludes CPU",
			state: gpuOnlyState,
			filterOptions: SuiteFilterOptions{
				ExcludeMissingWorkerKinds: true,
			},
			want: "~@cpu",
		},
		{
			name:  "custom suite tags are combined with option-driven filters",
			state: cpuAndGPUState,
			tags:  "@smoke && ~@slow",
			filterOptions: SuiteFilterOptions{
				ExcludeUnstable:           true,
				ExcludeMissingWorkerKinds: true,
			},
			want: "@smoke && ~@slow && ~@unstable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suite := testSampleSuite()
			suite.Tags = tt.tags
			suite.FilterOptions = tt.filterOptions
			runner, err := NewRunner(Options{
				KubectlContext:         "dev-context",
				TargetSoperatorVersion: testTargetSoperatorVersion,
				Suites:                 []SuiteConfig{suite},
				State:                  tt.state,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, runner.suiteTagFilter(runner.suites[0]))
		})
	}
}

func TestRunnerSuiteFeaturePaths(t *testing.T) {
	source := testFeatureSource()

	runner, err := NewRunner(Options{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: "4.1.5-reb85d0e5",
		Suites: []SuiteConfig{
			{
				Name:        "sample",
				Source:      source,
				VersionAxes: []versionfilter.Axis{SoperatorVersionAxis("4.1.5-reb85d0e5")},
			},
		},
	})
	require.NoError(t, err)
	paths, err := runner.suiteFeaturePaths(runner.suites[0])
	require.NoError(t, err)
	assert.Equal(t, []string{"features/sample.feature:3"}, paths)

	source.Paths = []string{"features/sample.feature:3", "features/sample.feature:7"}
	runner, err = NewRunner(Options{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Suites: []SuiteConfig{
			{
				Name:        "sample",
				Source:      source,
				VersionAxes: []versionfilter.Axis{SoperatorVersionAxis(testTargetSoperatorVersion)},
			},
		},
	})
	require.NoError(t, err)
	paths, err = runner.suiteFeaturePaths(runner.suites[0])
	require.NoError(t, err)
	assert.Equal(t, []string{"features/sample.feature:3", "features/sample.feature:7"}, paths)
}

func TestRunnerSuiteFeaturePathsAllowsUnversionedSuites(t *testing.T) {
	runner, err := NewRunner(Options{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Suites: []SuiteConfig{
			{Name: "custom", Source: testFeatureSource()},
		},
	})
	require.NoError(t, err)

	paths, err := runner.suiteFeaturePaths(runner.suites[0])
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
			_, err := NewRunner(Options{
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
	_, err := NewRunner(Options{
		KubectlContext: "dev-context",
		Suites:         []SuiteConfig{testSampleSuite()},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "target Soperator version is required")
}

func TestRunnerRunsDiscoveryHooks(t *testing.T) {
	state := &framework.ClusterState{}
	runner, err := NewRunner(Options{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Suites:                 []SuiteConfig{testSampleSuite()},
		State:                  state,
		DiscoveryHooks: []DiscoveryHook{
			func(ctx context.Context, state *framework.ClusterState, exec framework.Exec) error {
				state.Workers = []framework.WorkerRef{{Name: "worker-from-hook"}}
				return nil
			},
		},
	})
	require.NoError(t, err)

	require.NoError(t, runner.runDiscoveryHooks(context.Background(), nil))
	assert.Equal(t, []framework.WorkerRef{{Name: "worker-from-hook"}}, state.Workers)
}

func TestRunnerDiscoveryHookErrors(t *testing.T) {
	hookErr := errors.New("boom")
	runner, err := NewRunner(Options{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Suites:                 []SuiteConfig{testSampleSuite()},
		DiscoveryHooks: []DiscoveryHook{
			func(ctx context.Context, state *framework.ClusterState, exec framework.Exec) error {
				return hookErr
			},
		},
	})
	require.NoError(t, err)

	err = runner.runDiscoveryHooks(context.Background(), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, hookErr)
	assert.ErrorContains(t, err, "run discovery hook 0")
}

func TestRunnerRejectsNilDiscoveryHook(t *testing.T) {
	runner, err := NewRunner(Options{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Suites:                 []SuiteConfig{testSampleSuite()},
		DiscoveryHooks:         []DiscoveryHook{nil},
	})
	require.NoError(t, err)

	err = runner.runDiscoveryHooks(context.Background(), nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "discovery hook 0 is nil")
}

func TestRunnerRunConfiguredSuitesRunsAllSuitesAndAggregatesFailures(t *testing.T) {
	var passingRuns int
	runner, err := NewRunner(Options{
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

	err = runner.runConfiguredSuites(context.Background(), nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, `suite "failing": godog exited with status 1`)
	assert.Equal(t, 1, passingRuns)
}

func TestRunnerRunConfiguredSuitesSkipsEmptySuites(t *testing.T) {
	var passingRuns int
	runner, err := NewRunner(Options{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Suites: []SuiteConfig{
			{
				Name:        "empty",
				Source:      testFeatureSource(),
				VersionAxes: []versionfilter.Axis{SoperatorVersionAxis("3.0.0")},
			},
			{
				Name:           "passing",
				Source:         testRunnableFeatureSource("features/passing.feature", "a passing step runs"),
				StepRegistrars: []StepRegistrar{testRunnableStepRegistrar(&passingRuns)},
			},
		},
	})
	require.NoError(t, err)

	require.NoError(t, runner.runConfiguredSuites(context.Background(), nil))
	assert.Equal(t, 1, passingRuns)
}

func TestRunnerRunConfiguredSuitesErrorsWhenAllSuitesAreEmpty(t *testing.T) {
	runner, err := NewRunner(Options{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: testTargetSoperatorVersion,
		Suites: []SuiteConfig{
			{
				Name:        "empty",
				Source:      testFeatureSource(),
				VersionAxes: []versionfilter.Axis{SoperatorVersionAxis("3.0.0")},
			},
		},
	})
	require.NoError(t, err)

	err = runner.runConfiguredSuites(context.Background(), nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "no acceptance scenarios compatible with Soperator version 5.0.0")
}

func testSampleSuite() SuiteConfig {
	return SuiteConfig{
		Name:        "sample",
		Source:      testFeatureSource(),
		VersionAxes: []versionfilter.Axis{SoperatorVersionAxis(testTargetSoperatorVersion)},
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
	return func(sc *godog.ScenarioContext, state *framework.ClusterState, exec framework.Exec) {
		sc.Step(`^a passing step runs$`, func() error {
			*passingRuns = *passingRuns + 1
			return nil
		})
		sc.Step(`^a failing step runs$`, func() error {
			return errors.New("expected failure")
		})
	}
}
