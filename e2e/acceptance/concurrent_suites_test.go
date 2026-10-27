package acceptance

import (
	"context"
	"fmt"
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

func selectedScenarios(t *testing.T, suite SuiteConfig, version string) map[string]int {
	t.Helper()
	runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: version, Suites: []SuiteConfig{suite}})
	require.NoError(t, err)
	paths, err := runner.suiteFeaturePaths(&framework.ClusterInfo{TargetSoperatorVersion: version}, suite)
	require.NoError(t, err)
	selected := make(map[string]int)
	if len(paths) == 0 {
		return selected
	}
	parsed, err := (godog.TestSuite{Options: &godog.Options{FS: suite.Source.FS, Paths: paths, Tags: runner.suiteTagFilter(suite)}}).RetrieveFeatures()
	require.NoError(t, err)
	for _, feature := range parsed {
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

func TestConcurrentSuitesRunEachSelectedScenarioExactlyOnce(t *testing.T) {
	for _, version := range []string{"4.0.0", "5.0.0"} {
		for _, excludeHeavy := range []bool{false, true} {
			for _, excludeUnstable := range []bool{false, true} {
				suite := SoperatorSuite(version)
				suite.ExcludeHeavy, suite.ExcludeUnstable = excludeHeavy, excludeUnstable
				want := selectedScenarios(t, suite, version)
				actual := make(map[string]int)
				for _, split := range ConcurrentSuites(suite, 8) {
					for id, count := range selectedScenarios(t, split, version) {
						actual[id] += count
					}
				}
				assert.Equal(t, want, actual)
			}
		}
	}
	for _, path := range []string{"features/node_replacement.feature:3", "features/docker_containers.feature:36"} {
		suite := SoperatorSuite("5.0.0")
		suite.Source.Paths = []string{path}
		suite.ExcludeUnstable = false
		want := selectedScenarios(t, suite, "5.0.0")
		actual := make(map[string]int)
		for _, split := range ConcurrentSuites(suite, 8) {
			for id, count := range selectedScenarios(t, split, "5.0.0") {
				actual[id] += count
			}
		}
		assert.Equal(t, want, actual)
	}
}

func TestConcurrentSuitesKeepGlobalAssertionsSerial(t *testing.T) {
	suites := ConcurrentSuites(SoperatorSuite("5.0.0"), 8)
	suites[0].ExcludeUnstable = false
	serial := selectedScenarios(t, suites[0], "5.0.0")
	for _, name := range []string{"The provisioned cluster is ready", "A custom Slurm setting", "A worker template update", "A maintenance event replaces a CPU worker", "Manage jail dry-run", "Restarting a worker keeps shared Slurm"} {
		found := false
		for key := range serial {
			found = found || strings.Contains(key, name)
		}
		assert.True(t, found, "%s must remain exclusive", name)
	}
}

func TestConcurrentSuiteDoesNotStartAfterSerialFailure(t *testing.T) {
	var parallelRan atomic.Bool
	suite := SuiteConfig{
		Name:   "baseline",
		Source: FeatureSource{FS: fstest.MapFS{"baseline.feature": &fstest.MapFile{Data: []byte("Feature: baseline\n  Scenario: serial\n    Given the baseline fails\n  @parallel\n  Scenario: parallel\n    Given the parallel check runs\n")}}, Paths: []string{"baseline.feature"}},
		StepRegistrars: []StepRegistrar{func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
			sc.Step(`^the baseline fails$`, func() error { return fmt.Errorf("baseline unhealthy") })
			sc.Step(`^the parallel check runs$`, func() { parallelRan.Store(true) })
		}},
	}
	runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: "5.0.0", Suites: ConcurrentSuites(suite, 8)})
	require.NoError(t, err)
	err = runner.runConfiguredSuites(t.Context(), testClusterInfo(), nil, runner.suites)
	require.ErrorContains(t, err, "an earlier suite failed")
	assert.False(t, parallelRan.Load())
}

func TestConcurrentSuitesPreserveHookAndStepSkips(t *testing.T) {
	var skipCalls, passCalls, cleanupCalls atomic.Int32
	var feature strings.Builder
	feature.WriteString("Feature: skips\n")
	for _, tags := range []string{"", "@parallel"} {
		fmt.Fprintf(&feature, "  %s @skip\n  Scenario: tagged %s\n    Given a skipped continuation runs\n", tags, tags)
		for _, kind := range []string{"direct", "wrapped"} {
			fmt.Fprintf(&feature, "  %s\n  Scenario: %s %s\n    Given a %s skip is returned\n    Then a skipped continuation runs\n", tags, kind, tags, kind)
		}
	}
	feature.WriteString("  @parallel\n  Scenario: parallel continues\n    Given a passing check runs\n")
	suite := SuiteConfig{
		Name:   "skips",
		Source: FeatureSource{FS: fstest.MapFS{"skips.feature": {Data: []byte(feature.String())}}, Paths: []string{"skips.feature"}},
		StepRegistrars: []StepRegistrar{func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
			sc.Step(`^a (direct|wrapped) skip is returned$`, func(kind string) error {
				skipCalls.Add(1)
				if kind == "wrapped" {
					return fmt.Errorf("optional prerequisite absent: %w", godog.ErrSkip)
				}
				return godog.ErrSkip
			})
			sc.Step(`^a skipped continuation runs$`, func() error { return fmt.Errorf("execute a skipped step") })
			sc.Step(`^a passing check runs$`, func() { passCalls.Add(1) })
			sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
				cleanupCalls.Add(1)
				return ctx, nil
			})
		}},
	}
	runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: "5.0.0", OutputDir: t.TempDir(), Suites: ConcurrentSuites(suite, 8)})
	require.NoError(t, err)
	require.NoError(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), nil, runner.suites))
	assert.EqualValues(t, 4, skipCalls.Load())
	assert.EqualValues(t, 1, passCalls.Load(), "serial skips must allow the parallel suite to start")
	assert.EqualValues(t, 7, cleanupCalls.Load(), "cleanup must run for skipped scenarios")
}

func TestConcurrentSuitesDoNotHideCleanupFailureBehindSkip(t *testing.T) {
	var parallelRan atomic.Bool
	suite := SuiteConfig{
		Name:   "skip-cleanup",
		Source: FeatureSource{FS: fstest.MapFS{"skip.feature": {Data: []byte("Feature: cleanup\n  Scenario: skipped with cleanup failure\n    Given a skipped check leaves a cleanup failure\n  @parallel\n  Scenario: parallel\n    Given the parallel check runs\n")}}, Paths: []string{"skip.feature"}},
		StepRegistrars: []StepRegistrar{func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
			sc.Step(`^a skipped check leaves a cleanup failure$`, func(ctx context.Context) error {
				framework.RecordWorkerCleanupError(ctx, fmt.Errorf("restore worker baseline"))
				return godog.ErrSkip
			})
			sc.Step(`^the parallel check runs$`, func() { parallelRan.Store(true) })
		}},
	}
	runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: "5.0.0", Suites: ConcurrentSuites(suite, 8)})
	require.NoError(t, err)
	err = runner.runConfiguredSuites(t.Context(), testClusterInfo(), nil, runner.suites)
	require.ErrorContains(t, err, "restore worker baseline")
	assert.False(t, parallelRan.Load())
}

func TestGodogRunsSerialSuiteBeforeEightConcurrentScenarios(t *testing.T) {
	var feature strings.Builder
	feature.WriteString("Feature: scheduling\n  Scenario: serial\n    Given a scheduled check runs\n")
	for i := range 8 {
		fmt.Fprintf(&feature, "  @parallel\n  Scenario: parallel %d\n    Given a scheduled check runs\n", i)
	}
	var active atomic.Int32
	var serialFinished atomic.Bool
	barrier := make(chan struct{})
	type nameKey struct{}
	suite := SuiteConfig{
		Name: "scheduling", Source: FeatureSource{FS: fstest.MapFS{"scheduling.feature": &fstest.MapFile{Data: []byte(feature.String())}}, Paths: []string{"scheduling.feature"}},
		StepRegistrars: []StepRegistrar{func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
			sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
				return context.WithValue(ctx, nameKey{}, scenario.Name), nil
			})
			sc.Step(`^a scheduled check runs$`, func(ctx context.Context) error {
				if ctx.Value(nameKey{}) == "serial" {
					serialFinished.Store(true)
					return nil
				}
				if !serialFinished.Load() {
					return fmt.Errorf("serial suite did not finish first")
				}
				if active.Add(1) == 8 {
					close(barrier)
				}
				select {
				case <-barrier:
					return nil
				case <-time.After(3 * time.Second):
					return fmt.Errorf("eight scenarios did not run concurrently")
				}
			})
		}},
	}
	runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: "5.0.0", OutputDir: t.TempDir(), Suites: ConcurrentSuites(suite, 8)})
	require.NoError(t, err)
	require.NoError(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), nil, runner.suites))
	assert.EqualValues(t, 8, active.Load())
}
