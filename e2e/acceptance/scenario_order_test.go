package acceptance

import (
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
	ordered, err := heavyScenariosFirst(source, []string{"first.feature:5", "second.feature:3", "first.feature:8"})
	require.NoError(t, err)
	assert.Equal(t, []string{"second.feature:3", "first.feature:8", "first.feature:5"}, ordered, "exact selectors and outlines retain their original identity")
}
