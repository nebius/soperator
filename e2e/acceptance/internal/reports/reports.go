package reports

import (
	"fmt"
	"os"
	"path/filepath"
)

func Format(reportDir, suiteName string) (string, error) {
	if reportDir == "" {
		return "pretty", nil
	}
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir %q: %w", reportDir, err)
	}
	return fmt.Sprintf("pretty,cucumber:%s,junit:%s",
		filepath.Join(reportDir, suiteName+".cucumber.json"),
		filepath.Join(reportDir, suiteName+".junit.xml"),
	), nil
}
