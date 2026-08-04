package acceptance

import (
	"embed"
	"io/fs"

	"nebius.ai/soperator-e2e/versionfilter"
)

//go:embed features/*.feature
var acceptanceFeatures embed.FS

const SoperatorVersionTagPrefix = "@soperator_version_"

// FeatureSource describes the feature files Godog should run.
type FeatureSource struct {
	FS    fs.FS
	Paths []string
}

// SuiteFilterOptions configures tag filters that are convenient for the shared
// Soperator suite but can also be used by caller-owned suites when wanted.
type SuiteFilterOptions struct {
	// ExcludeUnstable excludes scenarios tagged @unstable from this suite.
	ExcludeUnstable bool

	// ExcludeMissingWorkerKinds excludes @gpu scenarios when no GPU workers are
	// discovered and @cpu scenarios when no CPU workers are discovered.
	ExcludeMissingWorkerKinds bool
}

// SuiteConfig describes one independently executed Godog suite.
type SuiteConfig struct {
	Name           string
	Source         FeatureSource
	VersionAxes    []versionfilter.Axis
	Tags           string
	StepRegistrars []StepRegistrar
	FilterOptions  SuiteFilterOptions
}

// SoperatorVersionAxis returns the standard Soperator scenario version axis.
func SoperatorVersionAxis(version string) versionfilter.Axis {
	return versionfilter.Axis{
		TagPrefix:     SoperatorVersionTagPrefix,
		TargetVersion: version,
	}
}

// SoperatorSuite returns the embedded open-source Soperator suite filtered by
// the target Soperator version.
func SoperatorSuite(targetSoperatorVersion string) SuiteConfig {
	return SuiteConfig{
		Name:           "soperator",
		Source:         SharedFeatureSource(),
		VersionAxes:    []versionfilter.Axis{SoperatorVersionAxis(targetSoperatorVersion)},
		StepRegistrars: []StepRegistrar{SharedStepRegistrar()},
		FilterOptions: SuiteFilterOptions{
			ExcludeUnstable:           true,
			ExcludeMissingWorkerKinds: true,
		},
	}
}

// SharedFeatureSource returns the embedded open-source Soperator acceptance features.
func SharedFeatureSource() FeatureSource {
	return FeatureSource{
		FS:    acceptanceFeatures,
		Paths: FeaturePaths(),
	}
}

// FeaturePaths returns the default embedded open-source Soperator feature paths.
func FeaturePaths() []string {
	return []string{
		"features/cluster_creation.feature",
		"features/observability.feature",
		"features/internal_ssh.feature",
		"features/package_installation.feature",
		"features/node_replacement.feature",
		"features/docker_containers.feature",
		"features/enroot_containers.feature",
		"features/topology.feature",
		"features/passive_checks.feature",
		"features/active_checks.feature",
		"features/system_checks.feature",
	}
}
