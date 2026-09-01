package acceptance

import (
	"embed"
	"io/fs"
)

//go:embed features/*.feature
var acceptanceFeatures embed.FS

// FeatureSource describes the feature files Godog should run.
type FeatureSource struct {
	FS    fs.FS
	Paths []string
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
		"features/soperator_utils.feature",
		"features/nodeset_ephemeral_mode_transition.feature",
		"features/node_replacement.feature",
		"features/docker_containers.feature",
		"features/enroot_containers.feature",
		"features/passive_checks.feature",
		"features/active_checks.feature",
		"features/system_checks.feature",
		"features/topology.feature",
		"features/topology_tree.feature",
		"features/topology_legacy.feature",
	}
}
