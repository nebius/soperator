package sharedsteps

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePAMSlurmAdoptStatus(t *testing.T) {
	ok, err := validatePAMSlurmAdoptStatus(
		"cgroup=0::/system.slice/slurmstepd.scope/job_42/step_extern/user\ngpu_count=1\n",
	)
	require.NoError(t, err)
	assert.True(t, ok)

	_, err = validatePAMSlurmAdoptStatus("cgroup=0::/pod/cgroup\ngpu_count=1\n")
	assert.ErrorContains(t, err, "step_extern")

	_, err = validatePAMSlurmAdoptStatus("cgroup=0::/job_42/step_extern\ngpu_count=2\n")
	assert.ErrorContains(t, err, "expected 1")

	_, err = validatePAMSlurmAdoptStatus("cgroup=0::/job_42/step_extern_child\ngpu_count=1\n")
	assert.ErrorContains(t, err, "step_extern")
}

func TestSSHCommandTimedOut(t *testing.T) {
	assert.True(t, sshCommandTimedOut(errors.New("command failed: exit status 124")))
	assert.True(t, sshCommandTimedOut(errors.New("command terminated with exit code 124")))
	assert.False(t, sshCommandTimedOut(errors.New("command failed: exit status 255")))
	assert.False(t, sshCommandTimedOut(nil))
}
