package sharedsteps

import "testing"

func TestRuntimePathsUnderAcceptsDockerInspectPaths(t *testing.T) {
	const root = "/mnt/image-storage/docker"
	output := `
/mnt/image-storage/docker/containers/ac9f51a97037/resolv.conf
/mnt/image-storage/docker/containers/ac9f51a97037/hostname
/mnt/image-storage/docker/containers/ac9f51a97037/hosts
/mnt/image-storage/docker/containers/ac9f51a97037/ac9f51a97037-json.log
`

	if !runtimePathsUnder(output, root) {
		t.Fatalf("runtimePathsUnder() = false, want true")
	}
}

func TestRuntimePathsUnderRejectsUnexpectedPaths(t *testing.T) {
	const root = "/mnt/image-storage/docker"
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "metadata without paths",
			output: "7a233c2a9c9881f79238b610bcfbe8b7bcd014a43b8c1dc17fcab60fa3e61c4c",
			want:   false,
		},
		{
			name:   "path outside root",
			output: "/var/lib/docker/containers/ac9f51a97037/hosts",
			want:   false,
		},
		{
			name: "one runtime path outside root",
			output: `/mnt/image-storage/docker/containers/ac9f51a97037/hosts
/var/lib/docker/containers/ac9f51a97037/hostname`,
			want: false,
		},
		{
			name:   "path under root",
			output: "/mnt/image-storage/docker/containers/ac9f51a97037/hosts",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimePathsUnder(tt.output, root); got != tt.want {
				t.Fatalf("runtimePathsUnder() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestPathIsUnder(t *testing.T) {
	const root = "/mnt/image-storage/docker"
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "root", value: "/mnt/image-storage/docker", want: true},
		{name: "child", value: "/mnt/image-storage/docker/overlay2/container/merged", want: true},
		{name: "sibling with common prefix", value: "/mnt/image-storage/docker-other", want: false},
		{name: "outside root", value: "/var/lib/docker", want: false},
		{name: "cleaned outside root", value: "/mnt/image-storage/docker/../containerd", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathIsUnder(tt.value, root); got != tt.want {
				t.Fatalf("pathIsUnder(%q, %q) = %t, want %t", tt.value, root, got, tt.want)
			}
		})
	}
}

func TestDockerCgroupParentBelongsToJob(t *testing.T) {
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
			if got := dockerCgroupParentBelongsToJob(test.value, test.jobID); got != test.wanted {
				t.Fatalf("dockerCgroupParentBelongsToJob(%q, %q) = %t, want %t", test.value, test.jobID, got, test.wanted)
			}
		})
	}
}
