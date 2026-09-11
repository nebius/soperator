package artifacts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testCollector struct {
	name string
	err  error
}

func (c testCollector) Name() string {
	return c.name
}

func (c testCollector) Collect(_ context.Context, destination string) error {
	err := os.WriteFile(filepath.Join(destination, "output.txt"), []byte(c.name), 0o600)
	return errors.Join(err, c.err)
}

func TestCollectAllPreservesOutputAndContinuesAfterFailure(t *testing.T) {
	root := t.TempDir()

	err := CollectAll(t.Context(), root,
		testCollector{name: "first / unsafe", err: errors.New("expected failure")},
		testCollector{name: "second"},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "expected failure")
	assert.FileExists(t, filepath.Join(root, "first-unsafe", "output.txt"))
	assert.FileExists(t, filepath.Join(root, "first-unsafe", "_error.txt"))
	assert.FileExists(t, filepath.Join(root, "second", "output.txt"))
}

func TestCollectAllRejectsEmptyOutputDir(t *testing.T) {
	err := CollectAll(t.Context(), "", testCollector{name: "collector"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "artifact output directory is required")
}

func TestCollectAllRejectsDuplicateSanitizedNames(t *testing.T) {
	err := CollectAll(t.Context(), t.TempDir(),
		testCollector{name: "same name"},
		testCollector{name: "same/name"},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "duplicated")
}
