package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

const libslurmReal = "libslurm.so.45.0.0"

func TestBindSlurmCommonPreservesCorrectSymlinks(t *testing.T) {
	jail, env := prepareBindSlurmCommonTest(t)
	runBindSlurmCommon(t, jail, env)

	libDir := filepath.Join(jail, "usr/lib/x86_64-linux-gnu")
	majorBefore, err := os.Lstat(filepath.Join(libDir, "libslurm.so.45"))
	require.NoError(t, err)
	unversionedBefore, err := os.Lstat(filepath.Join(libDir, "libslurm.so"))
	require.NoError(t, err)

	runBindSlurmCommon(t, jail, env)

	majorAfter, err := os.Lstat(filepath.Join(libDir, "libslurm.so.45"))
	require.NoError(t, err)
	unversionedAfter, err := os.Lstat(filepath.Join(libDir, "libslurm.so"))
	require.NoError(t, err)
	require.True(t, os.SameFile(majorBefore, majorAfter), "major-version symlink was replaced")
	require.True(t, os.SameFile(unversionedBefore, unversionedAfter), "unversioned symlink was replaced")
}

func TestBindSlurmCommonRepairsIncorrectSymlinks(t *testing.T) {
	jail, env := prepareBindSlurmCommonTest(t)
	libDir := filepath.Join(jail, "usr/lib/x86_64-linux-gnu")
	require.NoError(t, os.MkdirAll(libDir, 0o755))
	require.NoError(t, os.Symlink("old-libslurm.so", filepath.Join(libDir, "libslurm.so.45")))
	require.NoError(t, os.Symlink("old-libslurm.so", filepath.Join(libDir, "libslurm.so")))

	runBindSlurmCommon(t, jail, env)

	majorTarget, err := os.Readlink(filepath.Join(libDir, "libslurm.so.45"))
	require.NoError(t, err)
	unversionedTarget, err := os.Readlink(filepath.Join(libDir, "libslurm.so"))
	require.NoError(t, err)
	require.Equal(t, libslurmReal, majorTarget)
	require.Equal(t, libslurmReal, unversionedTarget)
}

func prepareBindSlurmCommonTest(t *testing.T) (string, []string) {
	t.Helper()

	tempDir := t.TempDir()
	jail := filepath.Join(tempDir, "jail")
	for _, dir := range []string{
		"usr/bin",
		"usr/lib/x86_64-linux-gnu",
		"usr/share/bash-completion/completions",
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(jail, dir), 0o755))
	}

	fakeBin := filepath.Join(tempDir, "bin")
	require.NoError(t, os.Mkdir(fakeBin, 0o755))
	writeExecutable(t, filepath.Join(fakeBin, "find"), "#!/bin/sh\nprintf '%s\\n' '"+libslurmReal+"'\n")
	writeExecutable(t, filepath.Join(fakeBin, "mount"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "uname"), "#!/bin/sh\nprintf '%s\\n' x86_64\n")

	env := append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return jail, env
}

func runBindSlurmCommon(t *testing.T, jail string, env []string) {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	script := filepath.Join(filepath.Dir(currentFile), "bind_slurm_common.sh")
	cmd := exec.CommandContext(t.Context(), "bash", script, "-j", jail)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o755))
}
