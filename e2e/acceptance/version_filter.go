package acceptance

import "nebius.ai/soperator-e2e/versionfilter"

const soperatorVersionTagPrefix = "@soperator_version_"

func selectCompatibleFeaturePaths(source FeatureSource, targetVersion string) ([]string, error) {
	return versionfilter.SelectScenarios(
		versionfilter.FeatureSource{
			FS:    source.FS,
			Paths: source.Paths,
		},
		versionfilter.Axis{
			TagPrefix:     soperatorVersionTagPrefix,
			TargetVersion: targetVersion,
		},
	)
}
