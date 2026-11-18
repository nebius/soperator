package sharedsteps

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobTmpfsBroadcastCommand(t *testing.T) {
	marker := "marker'; exit 99; '"
	command := `sbcast() {
    test "$1" = --jobid=42
    test "$2" = --force
    test "$(cat "$3")" = 42
    printf 'destination=%s\n' "$4"
}
srun() {
    test "$1" = --jobid=42
    shift 5
    test "$1" = /bin/sh
    test "$2" = -ec
    test "$4" = sh
    printf 'marker=%s\n' "$5"
    test "$6" = 42
}
` + jobTmpfsBroadcastCommand("42", marker)
	output, err := exec.CommandContext(t.Context(), "bash", "-c", command).CombinedOutput()
	require.NoError(t, err, string(output))
	assert.Equal(t, []string{
		"destination=/mnt/jail/mnt/memory/" + marker + ".broadcast",
		"destination=/mnt/jail/dev/shm/" + marker + ".broadcast",
		"destination=/mnt/jail/tmp/" + marker + ".broadcast",
		"marker=" + marker,
	}, strings.Split(strings.TrimSuffix(string(output), "\n"), "\n"))
}
