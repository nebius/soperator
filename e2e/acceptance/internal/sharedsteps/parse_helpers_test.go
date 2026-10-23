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
