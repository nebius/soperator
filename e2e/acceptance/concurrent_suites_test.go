package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func selectedCases(t *testing.T, suite SuiteConfig, version string) map[string]int {
	t.Helper()
	runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: version, Suites: []SuiteConfig{suite}})
	require.NoError(t, err)
	paths, err := runner.suiteFeaturePaths(&framework.ClusterInfo{TargetSoperatorVersion: version}, runner.suites[0])
	require.NoError(t, err)
	selected := make(map[string]int)
	if len(paths) == 0 {
		return selected
	}
	features, err := (godog.TestSuite{Options: &godog.Options{FS: suite.Source.FS, Paths: paths, Tags: runner.suiteTagFilter(suite)}}).RetrieveFeatures()
	require.NoError(t, err)
	for _, feature := range features {
		for _, scenario := range feature.Pickles {
			key := scenario.Uri + "/" + scenario.Name
			for _, step := range scenario.Steps {
				key += "/" + step.Text
			}
			selected[key]++
		}
	}
	return selected
}

func TestConcurrentSuitesPartitionSharedCasesExactlyOnce(t *testing.T) {
	for _, version := range []string{"4.1.14", "5.0.0"} {
		for _, heavy := range []bool{false, true} {
			for _, unstable := range []bool{false, true} {
				for _, manual := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/heavy=%t/unstable=%t/manual=%t", version, heavy, unstable, manual), func(t *testing.T) {
						suite := SoperatorSuite(version)
						suite.ExcludeHeavy, suite.ExcludeUnstable, suite.ExcludeManual = !heavy, !unstable, !manual
						want := selectedCases(t, suite, version)
						actual := make(map[string]int)
						for _, split := range ConcurrentSuites(suite, 1) {
							for key, count := range selectedCases(t, split, version) {
								actual[key] += count
							}
						}
						assert.Equal(t, want, actual)
						for key, count := range actual {
							assert.Equal(t, 1, count, key)
						}
					})
				}
			}
		}
	}
}

func TestConcurrentSuitesConfiguration(t *testing.T) {
	for _, reservations := range []bool{false, true} {
		suite := testSampleSuite()
		suite.Name = " sample "
		suite.Tags = "@smoke,@gpu"
		suite.WorkerReservations = reservations
		suites := ConcurrentSuites(suite, 3)
		require.Len(t, suites, 2)
		assert.Equal(t, "sample", suites[0].Name)
		assert.Equal(t, "sample-parallel", suites[1].Name)
		assert.Equal(t, 1, suites[0].Concurrency)
		assert.Equal(t, 3, suites[1].Concurrency)
		assert.False(t, suites[0].WorkerReservations)
		assert.Equal(t, reservations, suites[1].WorkerReservations)
		assert.Equal(t, "@smoke && @sequential,@gpu && @sequential", suites[0].Tags)
		assert.Equal(t, "@smoke && ~@sequential,@gpu && ~@sequential", suites[1].Tags)
		assert.Equal(t, "@smoke,@gpu", suite.Tags)
		assert.Equal(t, reservations, suite.WorkerReservations)
	}
}

func TestRunnerNormalizesConcurrency(t *testing.T) {
	for _, concurrency := range []int{-1, 0, 1, 3} {
		t.Run(fmt.Sprint(concurrency), func(t *testing.T) {
			suite := testSampleSuite()
			suite.Concurrency = concurrency
			runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: "5.0.0", Suites: []SuiteConfig{suite}})
			if concurrency < 0 {
				require.ErrorContains(t, err, "concurrency must not be negative")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, max(concurrency, 1), runner.suites[0].Concurrency)
		})
	}
}

func TestConcurrentSuitesPreserveSelectorsFiltersAndOrder(t *testing.T) {
	source := FeatureSource{
		FS: fstest.MapFS{
			"z.feature": {Data: []byte(`Feature: First feature
  @sequential @soperator_version_>=5.0.0 @product_version_>=1.0.0
  Scenario: serial first
    Given the case runs
  @smoke @soperator_version_>=5.0.0 @product_version_>=1.0.0
  Scenario: first
    Given the case runs
  @sequential @heavy @soperator_version_>=5.0.0 @product_version_>=1.0.0
  Scenario: serial heavy
    Given the case runs
  @gpu @heavy @soperator_version_>=5.0.0 @product_version_>=1.0.0
  Scenario: heavy
    Given the case runs
  @manual @soperator_version_>=5.0.0 @product_version_>=1.0.0
  Scenario: manual
    Given the case runs
  @unstable @soperator_version_>=5.0.0 @product_version_>=1.0.0
  Scenario: unstable
    Given the case runs
  @soperator_version_>=6.0.0 @product_version_>=1.0.0
  Scenario: future Soperator
    Given the case runs
  @soperator_version_>=5.0.0 @product_version_>=2.0.0
  Scenario: future product
    Given the case runs
`)},
			"a.feature": {Data: []byte(`Feature: Second feature
  @sequential @soperator_version_>=5.0.0 @product_version_>=1.0.0
  Scenario Outline: serial outline <value>
    Given the case runs
    Examples:
      | value |
      | one   |
      | two   |
  @smoke @soperator_version_>=5.0.0 @product_version_>=1.0.0
  Scenario Outline: outline <value>
    Given the case runs
    Examples:
      | value |
      | one   |
      | one   |
`)},
		},
		Paths: []string{"z.feature", "a.feature"},
	}
	for _, test := range []struct {
		name                          string
		paths                         []string
		tags                          string
		heavy, unstable, manual, axes bool
		want                          []string
	}{
		{name: "default", axes: true, want: []string{"serial first", "serial outline one", "serial outline two", "first", "outline one", "outline one"}},
		{name: "full", axes: true, heavy: true, unstable: true, manual: true, want: []string{"serial first", "serial heavy", "serial outline one", "serial outline two", "first", "heavy", "manual", "unstable", "outline one", "outline one"}},
		{name: "or filter", axes: true, heavy: true, tags: "@smoke,@gpu", want: []string{"first", "heavy", "outline one", "outline one"}},
		{name: "or filter respects exclusions", axes: true, tags: "@smoke,@gpu", want: []string{"first", "outline one", "outline one"}},
		{name: "file and overlapping lines", axes: true, paths: []string{"z.feature", "z.feature:6", "z.feature:3"}, want: []string{"serial first", "first"}},
		{name: "parallel line", axes: true, paths: []string{"z.feature:6"}, want: []string{"first"}},
		{name: "sequential line", axes: true, paths: []string{"z.feature:3"}, want: []string{"serial first"}},
		{name: "outline line", axes: true, paths: []string{"a.feature:10"}, want: []string{"outline one", "outline one"}},
		{name: "unversioned caller", paths: []string{"a.feature"}, want: []string{"serial outline one", "serial outline two", "outline one", "outline one"}},
		{name: "empty first suite", axes: true, tags: "@smoke", want: []string{"first", "outline one", "outline one"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var ran []string
			suite := SuiteConfig{Name: "selection", Source: source, Tags: test.tags, ExcludeHeavy: !test.heavy, ExcludeUnstable: !test.unstable, ExcludeManual: !test.manual}
			if test.paths != nil {
				suite.Source.Paths = test.paths
			}
			if test.axes {
				suite.VersionAxes = []ScenarioVersionAxis{SoperatorVersionAxis("5.0.0"), VersionAxis("@product_version_", "1.0.0")}
			}
			suite.StepRegistrars = []StepRegistrar{func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
				var name string
				sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
					name = scenario.Name
					return ctx, nil
				})
				sc.Step(`^the case runs$`, func() { ran = append(ran, name) })
			}}
			runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: "5.0.0", Suites: ConcurrentSuites(suite, 1)})
			require.NoError(t, err)
			require.NoError(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), nil, runner.suites))
			assert.Equal(t, test.want, ran)
		})
	}
}

func TestConcurrentSuiteRunsAfterSequentialFailure(t *testing.T) {
	for _, cleanupFailure := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleanup=%t", cleanupFailure), func(t *testing.T) {
			var ran []string
			failure := errors.New("sequential failure")
			suite := SuiteConfig{
				Name: "continue", Source: FeatureSource{FS: fstest.MapFS{"test.feature": {Data: []byte("Feature: continue\n  @sequential\n  Scenario: sequential\n    Given the sequential case runs\n  Scenario: parallel\n    Given the parallel case runs\n")}}, Paths: []string{"test.feature"}},
				StepRegistrars: []StepRegistrar{func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
					sc.Step(`^the sequential case runs$`, func(ctx context.Context) error {
						ran = append(ran, "sequential")
						if cleanupFailure {
							framework.RecordWorkerCleanupError(ctx, failure)
							return godog.ErrSkip
						}
						return failure
					})
					sc.Step(`^the parallel case runs$`, func() { ran = append(ran, "parallel") })
				}},
			}
			outputDir := t.TempDir()
			runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: "5.0.0", OutputDir: outputDir, Suites: ConcurrentSuites(suite, 1)})
			require.NoError(t, err)
			err = runner.runConfiguredSuites(t.Context(), testClusterInfo(), nil, runner.suites)
			require.Error(t, err)
			if cleanupFailure {
				require.ErrorIs(t, err, failure)
			}
			assert.Equal(t, []string{"sequential", "parallel"}, ran)
			for _, name := range []string{"continue", "continue-parallel"} {
				data, err := os.ReadFile(filepath.Join(outputDir, name+".cucumber.json"))
				require.NoError(t, err)
				var features []struct{ Elements []json.RawMessage }
				require.NoError(t, json.Unmarshal(data, &features))
				require.Len(t, features, 1)
				assert.Len(t, features[0].Elements, 1)
				assert.FileExists(t, filepath.Join(outputDir, name+".junit.xml"))
			}
		})
	}
}

func TestConcurrentSuitesPreserveSkipsAndCleanup(t *testing.T) {
	var feature strings.Builder
	feature.WriteString("Feature: skips\n")
	for _, tags := range []string{"@sequential", ""} {
		fmt.Fprintf(&feature, "  %s @skip\n  Scenario: tagged %s\n    Given a continuation runs\n", tags, tags)
		for _, kind := range []string{"direct", "wrapped", "hook"} {
			fmt.Fprintf(&feature, "  %s\n  Scenario: %s %s\n    Given a %s skip occurs\n    Then a continuation runs\n", tags, kind, tags, kind)
		}
	}
	var skips, cleanups int
	suite := SuiteConfig{
		Name: "skips", Source: FeatureSource{FS: fstest.MapFS{"test.feature": {Data: []byte(feature.String())}}, Paths: []string{"test.feature"}},
		StepRegistrars: []StepRegistrar{func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
			sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
				if strings.HasPrefix(scenario.Name, "hook") {
					skips++
					return ctx, fmt.Errorf("optional prerequisite: %w", godog.ErrSkip)
				}
				return ctx, nil
			})
			sc.Step(`^a (direct|wrapped|hook) skip occurs$`, func(kind string) error {
				skips++
				if kind == "wrapped" {
					return fmt.Errorf("optional prerequisite: %w", godog.ErrSkip)
				}
				return godog.ErrSkip
			})
			sc.Step(`^a continuation runs$`, func() error { return errors.New("run skipped continuation") })
			sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
				cleanups++
				return ctx, nil
			})
		}},
	}
	runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: "5.0.0", Suites: ConcurrentSuites(suite, 1)})
	require.NoError(t, err)
	require.NoError(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), nil, runner.suites))
	assert.Equal(t, 6, skips)
	assert.Equal(t, 8, cleanups)
}

func TestRunnerUsesNativeGodogConcurrencyWithIndependentState(t *testing.T) {
	const concurrency = 3
	var feature strings.Builder
	feature.WriteString("Feature: native scheduling\n  @sequential\n  Scenario: sequential\n    Given the sequential check runs\n")
	for i := range concurrency {
		fmt.Fprintf(&feature, "  Scenario: parallel %d\n    Given an independent check runs\n", i)
	}
	var sequentialFinished atomic.Bool
	var started atomic.Int32
	barrier := make(chan struct{})
	suite := SuiteConfig{
		Name: "native", Source: FeatureSource{FS: fstest.MapFS{"test.feature": {Data: []byte(feature.String())}}, Paths: []string{"test.feature"}},
		StepRegistrars: []StepRegistrar{func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
			var calls int
			sc.Step(`^the sequential check runs$`, func() { sequentialFinished.Store(true) })
			sc.Step(`^an independent check runs$`, func(ctx context.Context) error {
				if !sequentialFinished.Load() {
					return errors.New("start before sequential suite finished")
				}
				calls++
				if calls != 1 {
					return errors.New("share step state between scenarios")
				}
				if started.Add(1) == concurrency {
					close(barrier)
				}
				select {
				case <-barrier:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}},
	}
	runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: "5.0.0", Suites: ConcurrentSuites(suite, concurrency)})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, runner.runConfiguredSuites(ctx, testClusterInfo(), nil, runner.suites))
	assert.EqualValues(t, concurrency, started.Load())
}
