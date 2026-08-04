package framework

import (
	"fmt"

	"nebius.ai/soperator-e2e/versionfilter"
)

func NormalizeSoperatorVersion(raw string) (string, error) {
	version, err := versionfilter.NormalizeVersion(raw)
	if err != nil {
		return "", fmt.Errorf("Soperator %w", err)
	}
	return version, nil
}
