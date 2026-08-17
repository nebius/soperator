package acceptance

import "nebius.ai/soperator-e2e/acceptance/internal/versionfilter"

const SoperatorVersionTagPrefix = "@soperator_version_"

// ScenarioVersionAxis describes one scenario version tag axis.
type ScenarioVersionAxis struct {
	TagPrefix     string
	TargetVersion string
}

// SuiteConfig describes one independently executed Godog suite.
type SuiteConfig struct {
	Name            string
	Source          FeatureSource
	VersionAxes     []ScenarioVersionAxis
	Tags            string
	StepRegistrars  []StepRegistrar
	ExcludeUnstable bool
}

// VersionAxis returns a scenario version axis for product-owned version tags.
func VersionAxis(tagPrefix, targetVersion string) ScenarioVersionAxis {
	return ScenarioVersionAxis{
		TagPrefix:     tagPrefix,
		TargetVersion: targetVersion,
	}
}

// SoperatorVersionAxis returns the standard Soperator scenario version axis.
func SoperatorVersionAxis(version string) ScenarioVersionAxis {
	return VersionAxis(SoperatorVersionTagPrefix, version)
}

func (axis ScenarioVersionAxis) versionFilterAxis() versionfilter.Axis {
	return versionfilter.Axis{
		TagPrefix:     axis.TagPrefix,
		TargetVersion: axis.TargetVersion,
	}
}

// SoperatorSuite returns the embedded open-source Soperator suite filtered by
// the target Soperator version.
func SoperatorSuite(targetSoperatorVersion string) SuiteConfig {
	return SuiteConfig{
		Name:            "soperator",
		Source:          SharedFeatureSource(),
		VersionAxes:     []ScenarioVersionAxis{SoperatorVersionAxis(targetSoperatorVersion)},
		StepRegistrars:  []StepRegistrar{SharedStepRegistrar()},
		ExcludeUnstable: true,
	}
}
