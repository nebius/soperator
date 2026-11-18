package sharedsteps

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSlurmSeconds(t *testing.T) {
	for _, tt := range []struct {
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{raw: "90 sec", want: 90 * time.Second},
		{raw: " 0 sec ", want: 0},
		{raw: "NONE", want: 0},
		{raw: "90", wantErr: true},
		{raw: "90 min", wantErr: true},
		{raw: "-1 sec", wantErr: true},
		{raw: "x sec", wantErr: true},
		{raw: "", wantErr: true},
	} {
		got, err := parseSlurmSeconds(tt.raw)
		if tt.wantErr {
			assert.Error(t, err, "parseSlurmSeconds(%q)", tt.raw)
			continue
		}
		require.NoError(t, err, "parseSlurmSeconds(%q)", tt.raw)
		assert.Equal(t, tt.want, got, "parseSlurmSeconds(%q)", tt.raw)
	}
}

func TestParseKeyValueLine(t *testing.T) {
	output := "noise\nDOCKER_ROOT=/mnt/image-storage/docker\n  SESSION_CGROUP=/users/user-1000/sessions  \n"

	assert.Equal(t, "/mnt/image-storage/docker", parseKeyValueLine(output, "DOCKER_ROOT"))
	assert.Equal(t, "/users/user-1000/sessions", parseKeyValueLine(output, "SESSION_CGROUP"))
	assert.Empty(t, parseKeyValueLine(output, "MISSING"))
}

func TestParseKeyValues(t *testing.T) {
	values := parseKeyValues("start=1000.25\n  status=0 \nnot a pair\nstate:RUNNING=5\nstatus=1\nempty=\n")
	assert.Equal(t, map[string]string{"start": "1000.25", "status": "0"}, values,
		"keys are identifiers, values are non-empty, and the first line of a key wins")
}
