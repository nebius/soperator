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

func TestRuntimePathsUnderAcceptsHostVisibleJailPaths(t *testing.T) {
	const root = "/mnt/local-data/docker"
	output := `/mnt/jail/mnt/local-data/docker/containers/ac9f51a97037/resolv.conf
/mnt/jail/mnt/local-data/docker/containers/ac9f51a97037/hostname`

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

func TestDockerJailVisiblePath(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "jail visible", value: "/mnt/image-storage/docker", want: "/mnt/image-storage/docker"},
		{name: "host visible", value: "/mnt/jail/mnt/image-storage/docker", want: "/mnt/image-storage/docker"},
		{name: "jail root", value: "/mnt/jail", want: "/"},
		{name: "similar prefix", value: "/mnt/jail-other/docker", want: "/mnt/jail-other/docker"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dockerJailVisiblePath(tt.value)
			if err != nil {
				t.Fatalf("dockerJailVisiblePath() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("dockerJailVisiblePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDockerJailVisiblePathRejectsTraversal(t *testing.T) {
	if _, err := dockerJailVisiblePath("/mnt/jail/mnt/image-storage/../docker"); err == nil {
		t.Fatal("dockerJailVisiblePath() error = nil, want error")
	}
}

func TestDockerSrunTaskCommandNamesContainersPerTask(t *testing.T) {
	const want = `exec docker run --rm --name "e2e-docker-${SLURM_JOB_ID}-${SLURM_NODEID}-${SLURM_LOCALID}" ` +
		`'cr.nebius.cloud/soperator/busybox' sh -c 'echo ready; sleep 3600'`

	if got := dockerSrunTaskCommand(); got != want {
		t.Fatalf("dockerSrunTaskCommand() = %q, want %q", got, want)
	}
}
