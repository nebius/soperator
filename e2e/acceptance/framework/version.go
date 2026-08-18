package framework

import (
	"fmt"

	semver "github.com/Masterminds/semver/v3"

	"nebius.ai/soperator-e2e/acceptance/internal/versionfilter"
)

var soperatorFive = semver.MustParse("5.0.0")

func NormalizeSoperatorVersion(raw string) (string, error) {
	version, err := versionfilter.NormalizeVersion(raw)
	if err != nil {
		return "", fmt.Errorf("Soperator %w", err)
	}
	return version, nil
}

func SoperatorVersionBeforeFive(soperatorVersion string) bool {
	version, err := semver.StrictNewVersion(soperatorVersion)
	return err == nil && version.LessThan(soperatorFive)
}
