package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
