package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNodeLocalJailSubmountPermissionsCommand(t *testing.T) {
	command := nodeLocalJailSubmountPermissionsCommand(false)

	assert.Contains(t, command, "for path in /scratch")
	assert.NotContains(t, command, "/mnt/local-nvme")
	assert.Contains(t, command, `stat -c '%a'`)
	assert.Contains(t, command, `[ "$mode" != "777" ]`)

	command = nodeLocalJailSubmountPermissionsCommand(true)
	assert.Contains(t, command, "for path in /scratch /mnt/local-nvme")
}
