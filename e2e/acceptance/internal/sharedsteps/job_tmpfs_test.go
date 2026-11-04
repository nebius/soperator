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

func TestJobTmpfsJobCgroup(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{
			name: "legacy batch cgroup",
			path: "/slurm/uid_1000/job_42/step_batch/user/task_0",
			want: "/sys/fs/cgroup/slurm/uid_1000/job_42",
		},
		{
			name: "SLUID batch cgroup",
			path: "/system.slice/slurmstepd.scope/s8G5M22WGXB100/step_batch/user/task_0",
			want: "/sys/fs/cgroup/system.slice/slurmstepd.scope/s8G5M22WGXB100",
		},
		{
			name: "SLUID numeric step",
			path: "/system.slice/slurmstepd.scope/s8G5M22WGXB100/step_2/user/task_0",
			want: "/sys/fs/cgroup/system.slice/slurmstepd.scope/s8G5M22WGXB100",
		},
		{name: "another legacy job", path: "/slurm/uid_1000/job_43/step_batch/user/task_0"},
		{name: "unrecognized step", path: "/slurm/uid_1000/job_42/step_other/user/task_0"},
		{name: "SLUID outside Slurm scope", path: "/other.scope/s8G5M22WGXB100/step_batch/user/task_0"},
		{name: "malformed SLUID", path: "/slurmstepd.scope/s8G5M22WGXB10O/step_batch/user/task_0"},
		{name: "noncanonical path", path: "/slurm/uid_1000/job_42/../job_42/step_batch/user/task_0"},
		{name: "outside job", path: "/kubepods/container.scope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := jobTmpfsJobCgroup(tc.path, "42")
			if tc.want == "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
