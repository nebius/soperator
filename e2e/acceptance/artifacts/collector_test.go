package artifacts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type testCollector struct {
	name string
	err  error
}

type commonCollectorsRuntime struct {
	framework.Runtime
	args    framework.ArgsScope
	command framework.CommandScope
}

func (r *commonCollectorsRuntime) Kubectl() framework.ArgsScope       { return r.args }
func (r *commonCollectorsRuntime) Local() framework.ArgsScope         { return r.args }
func (r *commonCollectorsRuntime) Controller() framework.CommandScope { return r.command }

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

func TestCommonCollectorsIncludeOptionalMK8s(t *testing.T) {
	runtime := &commonCollectorsRuntime{
		args: framework.NewArgsScope(func(context.Context, ...string) (string, error) {
			return "", nil
		}),
		command: framework.NewCommandScope(func(context.Context, string) (string, error) {
			return "", nil
		}),
	}
	collectors := CommonCollectors(runtime, "")

	var names []string
	for _, collector := range collectors {
		names = append(names, collector.Name())
	}
	assert.Equal(t, []string{"kubernetes", "soperator", "fluxcd", "slurm", "jail", "mk8s"}, names)
}
