package acceptance

import (
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/internal/versionfilter"
)

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
	ExcludeHeavy    bool
	// ExcludeManual drops scenarios tagged @manual. Such scenarios are meant to run only when
	// a caller names them explicitly, so the CLI clears it when scenario selectors are given.
	ExcludeManual bool
	// Concurrency is the native Godog scenario concurrency. Zero defaults to one.
	Concurrency int
	// WorkerReservations opts this suite into native Slurm ownership and finalization.
	// Every worker-using caller in the suite must first acquire its complete set and bind every allocation to it.
	WorkerReservations bool
}

// ConcurrentSuites partitions a suite into @sequential scenarios followed by
// the remaining scenarios at the requested Godog concurrency. Both suites run
// even when the first fails. Native reservations remain opt-in on the second.
func ConcurrentSuites(suite SuiteConfig, concurrency int) []SuiteConfig {
	sequential, parallel := suite, suite
	sequential.Name = strings.TrimSpace(suite.Name)
	sequential.Concurrency = 1
	sequential.WorkerReservations = false
	sequential.Tags = combineTags(suite.Tags, "@sequential")
	parallel.Name = sequential.Name + "-parallel"
	parallel.Concurrency = concurrency
	parallel.Tags = combineTags(suite.Tags, "~@sequential")
	return []SuiteConfig{sequential, parallel}
}

func combineTags(left, right string) string {
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	// Godog uses comma-separated OR branches and does not support parentheses.
	var combined []string
	for _, a := range strings.Split(left, ",") {
		for _, b := range strings.Split(right, ",") {
			combined = append(combined, strings.TrimSpace(a)+" && "+strings.TrimSpace(b))
		}
	}
	return strings.Join(combined, ",")
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
		ExcludeManual:   true,
	}
}
