package framework

import (
	"fmt"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	"nebius.ai/soperator-e2e/versionfilter"
)

func NormalizeSoperatorVersion(raw string) (string, error) {
	version, err := versionfilter.NormalizeVersion(raw)
	if err != nil {
		return "", fmt.Errorf("Soperator %w", err)
	}
	return version, nil
}

func ParseSoperatorBaseVersion(raw string) (*semver.Version, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("Soperator version is required")
	}

	version, err := versionfilter.ParseBaseVersion(raw)
	if err != nil {
		return nil, fmt.Errorf("Soperator %w", err)
	}
	return version, nil
}
