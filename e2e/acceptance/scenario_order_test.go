package acceptance

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestConcurrentSuitesLaunchHeavyScenariosFirst(t *testing.T) {
	source := FeatureSource{
		FS: fstest.MapFS{
			"first.feature": {Data: []byte(`Feature: mixed scenarios
  @sequential
  Scenario: serial ordinary
    Given a check runs
  Scenario: first ordinary
    Given a check runs
  @heavy
  Scenario: first heavy
    Given a check runs
  @sequential @heavy
  Scenario: serial heavy
    Given a check runs
  Scenario: last ordinary
    Given a check runs
`)},
			"second.feature": {Data: []byte(`@heavy
Feature: inherited heavy scenarios
  Scenario Outline: heavy outline <value>
    Given check <value> runs
    Examples:
      | value |
      | one   |
      | two   |
  @unstable
  Scenario: excluded unstable heavy
    Given a check runs
`)},
		},
		Paths: []string{"first.feature", "second.feature"},
	}
	for _, excludeHeavy := range []bool{false, true} {
		suites := ConcurrentSuites(SuiteConfig{Name: "ordering", Source: source, ExcludeHeavy: excludeHeavy, ExcludeUnstable: true}, 4)
		runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: "5.0.0", Suites: suites})
		require.NoError(t, err)
		for i, suite := range runner.suites {
			paths, err := runner.suiteFeaturePaths(&framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"}, suite)
			require.NoError(t, err)
			features, err := (godog.TestSuite{Options: &godog.Options{FS: source.FS, Paths: paths, Tags: runner.suiteTagFilter(suite)}}).RetrieveFeatures()
			require.NoError(t, err)
			var names []string
			for _, feature := range features {
				for _, pickle := range feature.Pickles {
					names = append(names, pickle.Name)
				}
			}
			want := []string{"serial ordinary"}
			if i == 0 && !excludeHeavy {
				want = append(want, "serial heavy")
			}
			if i == 1 {
				want = []string{"first ordinary", "last ordinary"}
				if !excludeHeavy {
					want = append([]string{"first heavy", "heavy outline one", "heavy outline two"}, want...)
				}
			}
			assert.Equal(t, want, names, "suite=%s excludeHeavy=%t", suite.Name, excludeHeavy)
		}
	}
	ordered, err := orderScenarioPaths(source, []string{"first.feature:5", "second.feature:3", "first.feature:8"}, true, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"second.feature:3", "first.feature:8", "first.feature:5"}, ordered, "exact selectors and outlines retain their original identity")
}

func TestSeededOrderChangesParallelLaunchesAndPreservesSequentialOrder(t *testing.T) {
	var feature strings.Builder
	feature.WriteString("Feature: seeded order\n  @sequential\n  Scenario: serial first\n    Given check serial-first runs\n")
	var expected []string
	for i := range 10 {
		name := fmt.Sprintf("parallel-%d", i)
		expected = append(expected, name)
		fmt.Fprintf(&feature, "  Scenario: %s\n    Given check %s runs\n", name, name)
	}
	feature.WriteString("  @sequential\n  Scenario: serial last\n    Given check serial-last runs\n")
	source := FeatureSource{
		FS: fstest.MapFS{
			"first.feature":   {Data: []byte(feature.String())},
			"outline.feature": {Data: []byte("Feature: outline\n  Scenario Outline: outline <item>\n    Given check outline-<item> runs\n    Examples:\n      | item |\n      | a    |\n      | b    |\n")},
		},
		Paths: []string{"first.feature", "outline.feature"},
	}
	expected = append(expected, "outline-a", "outline-b")
	run := func(seed int64) []string {
		t.Helper()
		var launches []string
		base := SuiteConfig{
			Name: "seeded", Source: source, RandomSeed: seed,
			StepRegistrars: []StepRegistrar{func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
				sc.Step(`^check (.+) runs$`, func(name string) { launches = append(launches, name) })
			}},
		}
		runner, err := NewRunner(RunnerConfig{KubectlContext: "unused", TargetSoperatorVersion: "5.0.0", OutputDir: t.TempDir(), Suites: ConcurrentSuites(base, 1)})
		require.NoError(t, err)
		require.NoError(t, runner.runConfiguredSuites(t.Context(), testClusterInfo(), nil, runner.suites))
		require.Len(t, launches, len(expected)+2)
		assert.Equal(t, []string{"serial-first", "serial-last"}, launches[:2])
		assert.ElementsMatch(t, expected, launches[2:])
		return launches[2:]
	}
	first := run(104729)
	assert.Equal(t, first, run(104729), "a recorded seed must reproduce the launch queue and outline order")
	assert.NotEqual(t, first, run(130363), "different seeds must change the launch order across exact scenario selectors")
}

func TestRandomOrderRetainsVersionAndTagSelection(t *testing.T) {
	for _, version := range []string{"4.0.0", "5.0.0"} {
		for _, excludeHeavy := range []bool{false, true} {
			base := SoperatorSuite(version)
			base.ExcludeHeavy = excludeHeavy
			want := selectedScenarios(t, base, version)
			base.RandomSeed = 104729
			actual := make(map[string]int)
			for _, suite := range ConcurrentSuites(base, 4) {
				for id, count := range selectedScenarios(t, suite, version) {
					actual[id] += count
				}
			}
			assert.Equal(t, want, actual)
		}
	}
}
