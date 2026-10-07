package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectJobTmpfsWorker(t *testing.T) {
	worker, err := selectJobTmpfsWorker("worker-0 7/1/0/8\nworker-1 3/5/0/8\nworker-2 0/8/0/8\n")
	require.NoError(t, err)
	assert.Equal(t, "worker-1", worker)

	for _, output := range []string{"", "worker-0 7/1/0/8\n", "worker-0 invalid\n", "worker-0 0/nan/0/8\n"} {
		_, err := selectJobTmpfsWorker(output)
		assert.ErrorContains(t, err, "two idle CPUs")
	}
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
