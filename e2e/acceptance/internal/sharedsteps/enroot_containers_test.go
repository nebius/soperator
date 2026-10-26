package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOSReleaseID(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "unquoted ID",
			output: "NAME=Ubuntu\nID=ubuntu\nID_LIKE=debian\n",
			want:   "ubuntu",
		},
		{
			name:   "quoted ID",
			output: "NAME=Ubuntu\nID=\"ubuntu\"\n",
			want:   "ubuntu",
		},
		{
			name:   "missing ID",
			output: "NAME=Ubuntu\nID_LIKE=debian\n",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := osReleaseID(tt.output); got != tt.want {
				t.Fatalf("osReleaseID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseConfiguredEnrootDataPath(t *testing.T) {
	path, err := parseConfiguredEnrootDataPath(" /mnt/local-data/enroot/data\n")
	require.NoError(t, err)
	assert.Equal(t, "/mnt/local-data/enroot/data", path)
}

func TestParseConfiguredEnrootDataPathRejectsUnsupportedValues(t *testing.T) {
	for _, value := range []string{"", "relative/path", "/mnt/local-data/../data", "/run/enroot/user-$(id -u)"} {
		t.Run(value, func(t *testing.T) {
			_, err := parseConfiguredEnrootDataPath(value)
			assert.Error(t, err)
		})
	}
}
