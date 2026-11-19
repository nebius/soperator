package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlurmCgroupParentBelongsToJob(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		jobID  string
		wanted bool
	}{
		{
			name:   "Slurm job user cgroup",
			value:  "/kubepods.slice/pod.scope/slurm/uid_1000/job_42/step_0/user",
			jobID:  "42",
			wanted: true,
		},
		{
			name:   "Slurm 26.05 SLUID user cgroup",
			value:  "/kubepods.slice/pod.scope/system.slice/slurmstepd.scope/s8G5M22WGXB100/step_0/user",
			jobID:  "42",
			wanted: true,
		},
		{
			name:   "different Slurm job",
			value:  "/kubepods.slice/pod.scope/slurm/uid_1000/job_43/step_0/user",
			jobID:  "42",
			wanted: false,
		},
		{
			name:   "task cgroup instead of user parent",
			value:  "/kubepods.slice/pod.scope/slurm/uid_1000/job_42/step_0/user/task_0",
			jobID:  "42",
			wanted: false,
		},
		{
			name:   "SLUID outside slurmstepd scope",
			value:  "/kubepods.slice/pod.scope/other.scope/s8G5M22WGXB100/step_0/user",
			jobID:  "42",
			wanted: false,
		},
		{
			name:   "malformed SLUID",
			value:  "/kubepods.slice/pod.scope/system.slice/slurmstepd.scope/s8G5M22WGXB10O/step_0/user",
			jobID:  "42",
			wanted: false,
		},
		{
			name:   "unknown step",
			value:  "/kubepods.slice/pod.scope/system.slice/slurmstepd.scope/s8G5M22WGXB100/step_other/user",
			jobID:  "42",
			wanted: false,
		},
		{
			name:   "missing job ID",
			value:  "/kubepods.slice/pod.scope/system.slice/slurmstepd.scope/s8G5M22WGXB100/step_0/user",
			jobID:  "",
			wanted: false,
		},
		{
			name:   "dockerd default",
			value:  "",
			jobID:  "42",
			wanted: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := slurmCgroupParentBelongsToJob(test.value, test.jobID); got != test.wanted {
				t.Fatalf("slurmCgroupParentBelongsToJob(%q, %q) = %t, want %t", test.value, test.jobID, got, test.wanted)
			}
		})
	}
}

func TestSlurmJobCgroup(t *testing.T) {
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
			got, err := slurmJobCgroup(tc.path, "42")
			if tc.want == "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
