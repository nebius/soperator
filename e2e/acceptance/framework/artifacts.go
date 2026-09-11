package framework

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const AcceptanceScenarioArtifactsDir = "/opt/soperator-outputs/shared/acceptance/scenarios"

const maxArtifactPathPartLength = 80

var invalidArtifactPathPart = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

type scenarioArtifactsCtxKey struct{}

// ScenarioArtifactPaths contains the runner and jail directories assigned to one scenario.
type ScenarioArtifactPaths struct {
	RunnerDir string
	JailDir   string
}

// ArtifactsManager prepares per-scenario artifact directories.
type ArtifactsManager struct {
	outputDir string
	jail      CommandScope
}

// NewArtifactsManager creates a scenario artifact manager.
func NewArtifactsManager(outputDir string, jail CommandScope) *ArtifactsManager {
	return &ArtifactsManager{
		outputDir: strings.TrimSpace(outputDir),
		jail:      jail,
	}
}

// PrepareScenario creates and attaches the artifact paths for one scenario.
func (m *ArtifactsManager) PrepareScenario(
	ctx context.Context,
	suiteName string,
	scenarioURI string,
	scenarioName string,
	scenarioID string,
) (context.Context, error) {
	if m == nil || m.outputDir == "" {
		return ctx, nil
	}

	suitePart := sanitizeArtifactPathPart(suiteName)
	scenarioPart := scenarioArtifactPathPart(scenarioURI, scenarioName, scenarioID)
	paths := ScenarioArtifactPaths{
		RunnerDir: filepath.Join(m.outputDir, "scenarios", suitePart, scenarioPart),
		JailDir:   path.Join(AcceptanceScenarioArtifactsDir, suitePart, scenarioPart),
	}
	ctx = context.WithValue(ctx, scenarioArtifactsCtxKey{}, paths)

	var failures []error
	if err := os.MkdirAll(paths.RunnerDir, 0o755); err != nil {
		failures = append(failures, fmt.Errorf("create runner artifact directory %q: %w", paths.RunnerDir, err))
	}
	if m.jail == nil {
		failures = append(failures, fmt.Errorf("create jail artifact directory %q: jail scope is unavailable", paths.JailDir))
	} else {
		command := fmt.Sprintf("install -d -m 0777 %s", ShellQuote(paths.JailDir))
		if _, err := m.jail.Run(ctx, command); err != nil {
			failures = append(failures, fmt.Errorf("create jail artifact directory %q: %w", paths.JailDir, err))
		}
	}

	return ctx, errors.Join(failures...)
}

// ScenarioArtifacts returns the artifact paths assigned to the current scenario.
func ScenarioArtifacts(ctx context.Context) (ScenarioArtifactPaths, bool) {
	paths, ok := ctx.Value(scenarioArtifactsCtxKey{}).(ScenarioArtifactPaths)
	return paths, ok
}

func scenarioArtifactPathPart(uri, name, id string) string {
	uriPart := sanitizeArtifactPathPart(path.Base(strings.TrimSpace(uri)))
	namePart := sanitizeArtifactPathPart(name)
	hash := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%s-%s-%x", uriPart, namePart, hash[:4])
}

func sanitizeArtifactPathPart(value string) string {
	part := invalidArtifactPathPart.ReplaceAllString(strings.TrimSpace(value), "-")
	part = strings.Trim(part, "-._")
	if part == "" {
		part = "unknown"
	}
	if len(part) > maxArtifactPathPartLength {
		part = strings.TrimRight(part[:maxArtifactPathPartLength], "-._")
	}
	return part
}
