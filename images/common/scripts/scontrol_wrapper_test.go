package scripts_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func scontrolWrapperPath(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(currentFile), "scontrol_wrapper.sh")
}

func TestScontrolWrapperNeedsWarning(t *testing.T) {
	wrapper := scontrolWrapperPath(t)
	for _, tt := range []struct {
		args []string
		want bool
	}{
		{args: nil, want: false},
		{args: []string{"show", "partitions"}, want: false},
		{args: []string{"ping"}, want: false},
		{args: []string{"reconfigure"}, want: false},
		{args: []string{"update", "NodeName=w-0", "State=drain", "Reason=x"}, want: false},
		{args: []string{"update", "NodeName=w-0", "State=resume"}, want: false},
		{args: []string{"create", "ReservationName=r", "Nodes=w-0", "Duration=1"}, want: false},
		{args: []string{"update", "JobId=1", "TimeLimit=10"}, want: false},
		{args: []string{"update", "JobId=1", "Features=h100"}, want: false},
		{args: []string{"update", "ReservationName=r", "Features=ib"}, want: false},
		{args: []string{"update", "Gres=gpu:8"}, want: false},
		{args: []string{"c", "PartitionName=x"}, want: false},
		{args: []string{"update", "PartitionName=main", "State=DOWN"}, want: true},
		{args: []string{"CREATE", "partitionname=spot", "Nodes=w-[0-3]"}, want: true},
		{args: []string{"delete", "PartitionName=spot"}, want: true},
		{args: []string{"-o", "upd", "PartitionName=x"}, want: true},
		{args: []string{"-M", "c1", "delete", "PartitionName=x"}, want: true},
		{args: []string{"--clusters", "c1", "--json", "update", "PartitionName=x"}, want: true},
		{args: []string{"cr", "PartitionName=x"}, want: true},
		{args: []string{"update", "NodeName=w-0", "Gres=gpu:8"}, want: true},
		{args: []string{"update", "NodeName=w-0", "Weight=5"}, want: true},
		{args: []string{"update", "NodeName=w-0", "Features=a"}, want: true},
		{args: []string{"update", "NodeName=w-0", "AvailableFeatures=a", "ActiveFeatures=a"}, want: true},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			cmdArgs := append([]string{"-c", "source \"$1\"; shift; needs_warning \"$@\"", "_", wrapper}, tt.args...)
			cmd := exec.CommandContext(t.Context(), "bash", cmdArgs...)
			output, err := cmd.CombinedOutput()
			require.Empty(t, string(output))
			if tt.want {
				require.NoError(t, err)
				return
			}
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			require.Equal(t, 1, exitErr.ExitCode())
		})
	}
}

func TestScontrolWrapperExecsReal(t *testing.T) {
	source, err := os.ReadFile(scontrolWrapperPath(t))
	require.NoError(t, err)

	for _, tt := range []struct {
		name  string
		args  []string
		exit  int
		quiet bool
		warn  bool
	}{
		{name: "config change warns and passes through", args: []string{"update", "PartitionName=x", "State=UP"}, warn: true},
		{name: "read-only command passes through silently", args: []string{"show", "config"}},
		{name: "exit code propagates", args: []string{"update", "PartitionName=x"}, exit: 7, warn: true},
		{name: "quiet variable suppresses the warning", args: []string{"update", "PartitionName=x"}, quiet: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			argsFile := filepath.Join(dir, "args")
			mockReal := filepath.Join(dir, "scontrol.real")
			writeExecutable(t, mockReal, "#!/bin/bash\nprintf '%s\\n' \"$@\" > \"$MOCK_ARGS_FILE\"\nexit \"$MOCK_EXIT\"\n")
			script := filepath.Join(dir, "scontrol")
			writeExecutable(t, script, strings.ReplaceAll(string(source), "/usr/bin/scontrol.real", mockReal))

			cmd := exec.CommandContext(t.Context(), "bash", append([]string{script}, tt.args...)...)
			cmd.Env = append(os.Environ(), "MOCK_ARGS_FILE="+argsFile, "MOCK_EXIT="+strconv.Itoa(tt.exit))
			if tt.quiet {
				cmd.Env = append(cmd.Env, "SOPERATOR_SCONTROL_QUIET=1")
			}
			var stdout, stderr strings.Builder
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			if tt.exit == 0 {
				require.NoError(t, err)
			} else {
				var exitErr *exec.ExitError
				require.True(t, errors.As(err, &exitErr))
				require.Equal(t, tt.exit, exitErr.ExitCode())
			}
			require.Empty(t, stdout.String(), "the wrapper must not write to stdout")
			if tt.warn {
				require.Contains(t, stderr.String(), "WARNING: this scontrol change is not persistent")
				require.Contains(t, stderr.String(), "SOPERATOR_SCONTROL_QUIET=1")
			} else {
				require.Empty(t, stderr.String())
			}

			got, err := os.ReadFile(argsFile)
			require.NoError(t, err)
			require.Equal(t, strings.Join(tt.args, "\n")+"\n", string(got))
		})
	}
}
