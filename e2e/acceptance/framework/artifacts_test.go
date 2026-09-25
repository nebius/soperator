package framework

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type artifactCommandScope struct {
	command string
	err     error
}

func (s *artifactCommandScope) Run(_ context.Context, command string) (string, error) {
	s.command = command
	return "", s.err
}

func (s *artifactCommandScope) RunWithRetry(ctx context.Context, command string, _ int, _ time.Duration) (string, error) {
	return s.Run(ctx, command)
}

func (s *artifactCommandScope) RunWithDefaultRetry(ctx context.Context, command string) (string, error) {
	return s.Run(ctx, command)
}

func TestArtifactsManagerPreparesScenarioPaths(t *testing.T) {
	jail := &artifactCommandScope{}
	manager := NewArtifactsManager(t.TempDir(), jail)

	ctx, err := manager.PrepareScenario(
		context.Background(),
		"managed/suite",
		"features/passive checks.feature",
		"drop_page_cache runs",
		"pickle-1",
	)
	require.NoError(t, err)

	paths, ok := ScenarioArtifacts(ctx)
	require.True(t, ok)
	assert.DirExists(t, paths.RunnerDir)
	assert.Contains(t, filepath.ToSlash(paths.RunnerDir), "/scenarios/managed-suite/")
	assert.Equal(t,
		"/opt/soperator-outputs/shared/acceptance/scenarios/managed-suite/passive-checks.feature-drop_page_cache-runs-6a180a5b",
		paths.JailDir,
	)
	assert.Contains(t, jail.command, "install -d -m 0777")
	assert.Contains(t, jail.command, paths.JailDir)
}

func TestArtifactsManagerAttachesPathsWhenJailCreationFails(t *testing.T) {
	jail := &artifactCommandScope{err: errors.New("jail unavailable")}
	manager := NewArtifactsManager(t.TempDir(), jail)

	ctx, err := manager.PrepareScenario(context.Background(), "suite", "feature", "scenario", "id")
	require.Error(t, err)
	assert.ErrorContains(t, err, "jail unavailable")

	paths, ok := ScenarioArtifacts(ctx)
	require.True(t, ok)
	assert.DirExists(t, paths.RunnerDir)
}

func TestArtifactsManagerIsDisabledWithoutOutputDir(t *testing.T) {
	manager := NewArtifactsManager("", &artifactCommandScope{})

	ctx, err := manager.PrepareScenario(context.Background(), "suite", "feature", "scenario", "id")
	require.NoError(t, err)
	_, ok := ScenarioArtifacts(ctx)
	assert.False(t, ok)
}
