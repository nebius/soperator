package versionfilter

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	soperatorVersionTag        = "@soperator_version_"
	anotherSoperatorVersionTag = "@another_soperator_version_"
)

func TestSelectScenariosFiltersByTargetVersion(t *testing.T) {
	features := FeatureSource{
		FS: fstest.MapFS{
			"features/sample.feature": {Data: []byte(`Feature: Sample
  @soperator_version_>=4.0.0
  Scenario: old scenario
    Then old behavior works

  @soperator_version_>=5.0.0
  Scenario: new scenario
    Then new behavior works

  @soperator_version_>=4.0.0,<4.1.0||>=5.0.0
  Scenario: split range scenario
    Then split range behavior works
`)},
		},
		Paths: []string{"features/sample.feature"},
	}

	paths, err := SelectScenarios(features, Axis{TagPrefix: soperatorVersionTag, TargetVersion: "4.1.5-reb85d0e5"})
	require.NoError(t, err)
	assert.Equal(t, []string{"features/sample.feature:3"}, paths)

	paths, err = SelectScenarios(features, Axis{TagPrefix: soperatorVersionTag, TargetVersion: "5.0.0"})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"features/sample.feature:3",
		"features/sample.feature:7",
		"features/sample.feature:11",
	}, paths)
}

func TestSelectScenariosAcceptsExactScenarioLine(t *testing.T) {
	features := FeatureSource{
		FS: fstest.MapFS{
			"features/sample.feature": {Data: []byte(`Feature: Sample
  @soperator_version_>=4.0.0
  Scenario: old scenario
    Then old behavior works
`)},
		},
		Paths: []string{"features/sample.feature:3"},
	}

	paths, err := SelectScenarios(features, Axis{TagPrefix: soperatorVersionTag, TargetVersion: "4.0.0"})
	require.NoError(t, err)
	assert.Equal(t, []string{"features/sample.feature:3"}, paths)
}

func TestSelectScenariosRejectsTagOrBodyLine(t *testing.T) {
	features := FeatureSource{
		FS: fstest.MapFS{
			"features/sample.feature": {Data: []byte(`Feature: Sample
  @soperator_version_>=4.0.0
  Scenario: old scenario
    Then old behavior works
`)},
		},
		Paths: []string{"features/sample.feature:2"},
	}

	_, err := SelectScenarios(features, Axis{TagPrefix: soperatorVersionTag, TargetVersion: "4.0.0"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "line-based selection must point to the Scenario line")

	features.Paths = []string{"features/sample.feature:4"}
	_, err = SelectScenarios(features, Axis{TagPrefix: soperatorVersionTag, TargetVersion: "4.0.0"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "line-based selection must point to the Scenario line")
}

func TestSelectScenariosRequiresScenarioLevelVersionTag(t *testing.T) {
	features := FeatureSource{
		FS: fstest.MapFS{
			"features/sample.feature": {Data: []byte(`@soperator_version_>=4.0.0
Feature: Sample
  Scenario: old scenario
    Then old behavior works
`)},
		},
		Paths: []string{"features/sample.feature"},
	}

	_, err := SelectScenarios(features, Axis{TagPrefix: soperatorVersionTag, TargetVersion: "4.0.0"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "must have exactly one @soperator_version_")
}

func TestSelectScenariosRequiresAllAxes(t *testing.T) {
	features := FeatureSource{
		FS: fstest.MapFS{
			"features/another-soperator.feature": {Data: []byte(`Feature: Another Soperator
  @soperator_version_>=5.0.0 @another_soperator_version_>=1.0.0
  Scenario: compatible scenario
    Then another Soperator behavior works

  @soperator_version_>=5.0.0 @another_soperator_version_>=2.0.0
  Scenario: future another Soperator scenario
    Then future another Soperator behavior works
`)},
		},
		Paths: []string{"features/another-soperator.feature"},
	}

	paths, err := SelectScenarios(
		features,
		Axis{TagPrefix: soperatorVersionTag, TargetVersion: "5.0.0"},
		Axis{TagPrefix: anotherSoperatorVersionTag, TargetVersion: "1.5.0"},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"features/another-soperator.feature:3"}, paths)
}

func TestSelectScenariosRejectsMissingAxisTag(t *testing.T) {
	features := FeatureSource{
		FS: fstest.MapFS{
			"features/another-soperator.feature": {Data: []byte(`Feature: Another Soperator
  @soperator_version_>=5.0.0
  Scenario: missing another Soperator version
    Then another Soperator behavior works
`)},
		},
		Paths: []string{"features/another-soperator.feature"},
	}

	_, err := SelectScenarios(
		features,
		Axis{TagPrefix: soperatorVersionTag, TargetVersion: "5.0.0"},
		Axis{TagPrefix: anotherSoperatorVersionTag, TargetVersion: "1.0.0"},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "must have exactly one @another_soperator_version_")
}

func TestSelectScenariosRejectsMissingVersionTag(t *testing.T) {
	features := FeatureSource{
		FS: fstest.MapFS{
			"features/sample.feature": {Data: []byte(`Feature: Sample
  Scenario: untagged scenario
    Then behavior works
`)},
		},
		Paths: []string{"features/sample.feature"},
	}

	_, err := SelectScenarios(features, Axis{TagPrefix: soperatorVersionTag, TargetVersion: "5.0.0"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "must have exactly one @soperator_version_")
}

func TestSelectScenariosRejectsDuplicateVersionTags(t *testing.T) {
	features := FeatureSource{
		FS: fstest.MapFS{
			"features/sample.feature": {Data: []byte(`Feature: Sample
  @soperator_version_>=4.0.0 @soperator_version_<5.0.0
  Scenario: duplicated version tags
    Then behavior works
`)},
		},
		Paths: []string{"features/sample.feature"},
	}

	_, err := SelectScenarios(features, Axis{TagPrefix: soperatorVersionTag, TargetVersion: "5.0.0"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "got 2")
}

func TestSelectScenariosRejectsShortVersionConstraint(t *testing.T) {
	features := FeatureSource{
		FS: fstest.MapFS{
			"features/sample.feature": {Data: []byte(`Feature: Sample
  @soperator_version_>=5.0
  Scenario: short version
    Then behavior works
`)},
		},
		Paths: []string{"features/sample.feature"},
	}

	_, err := SelectScenarios(features, Axis{TagPrefix: soperatorVersionTag, TargetVersion: "5.0.0"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "full major.minor.patch")
}

func TestSelectScenariosRejectsMissingAxes(t *testing.T) {
	features := FeatureSource{
		FS: fstest.MapFS{
			"features/sample.feature": {Data: []byte(`Feature: Sample
  @soperator_version_>=5.0.0
  Scenario: tagged
    Then behavior works
`)},
		},
		Paths: []string{"features/sample.feature"},
	}

	_, err := SelectScenarios(features)
	require.Error(t, err)
	assert.ErrorContains(t, err, "at least one version axis is required")
}

func TestNormalizeVersion(t *testing.T) {
	version, err := NormalizeVersion("4.1.5-reb85d0e5")
	require.NoError(t, err)
	assert.Equal(t, "4.1.5", version)

	version, err = NormalizeVersion("v5.0.0+build.1")
	require.NoError(t, err)
	assert.Equal(t, "5.0.0", version)
}
