package reports

import (
	"fmt"
	"os"
	"path/filepath"
)

// TeamCityEnabled reports whether the runner is executing under TeamCity.
func TeamCityEnabled() bool {
	return os.Getenv("TEAMCITY_VERSION") != ""
}

func Format(reportDir, suiteName string) (string, error) {
	if reportDir == "" {
		return "pretty", nil
	}
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir %q: %w", reportDir, err)
	}
	junitDir := reportDir
	if TeamCityEnabled() {
		junitDir = filepath.Join(reportDir, "junit")
		if err := os.MkdirAll(junitDir, 0o755); err != nil {
			return "", fmt.Errorf("create JUnit report dir %q: %w", junitDir, err)
		}
	}
	return fmt.Sprintf("pretty,cucumber:%s,junit:%s",
		filepath.Join(reportDir, suiteName+".cucumber.json"),
		filepath.Join(junitDir, suiteName+".junit.xml"),
	), nil
}
