package steps

import "testing"

func TestGraphDriverPathsUnderAcceptsDockerOverlayInspectData(t *testing.T) {
	const root = "/mnt/image-storage/docker"
	output := `
7a233c2a9c9881f79238b610bcfbe8b7bcd014a43b8c1dc17fcab60fa3e61c4c
/mnt/image-storage/docker/overlay2/init/diff:/mnt/image-storage/docker/overlay2/base/diff
/mnt/image-storage/docker/overlay2/container/merged
/mnt/image-storage/docker/overlay2/container/diff
/mnt/image-storage/docker/overlay2/container/work
`

	if !graphDriverPathsUnder(output, root) {
		t.Fatalf("graphDriverPathsUnder() = false, want true")
	}
}

func TestGraphDriverPathsUnderRejectsUnexpectedPaths(t *testing.T) {
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
			output: "/var/lib/docker/overlay2/container/merged",
			want:   false,
		},
		{
			name:   "one lowerdir outside root",
			output: "/mnt/image-storage/docker/overlay2/base/diff:/var/lib/docker/overlay2/base/diff",
			want:   false,
		},
		{
			name:   "path under root",
			output: "/mnt/image-storage/docker/overlay2/container/merged",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := graphDriverPathsUnder(tt.output, root); got != tt.want {
				t.Fatalf("graphDriverPathsUnder() = %t, want %t", got, tt.want)
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
