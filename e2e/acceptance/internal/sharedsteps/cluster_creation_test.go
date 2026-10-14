package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestNodeLocalJailSubmountPermissionsCommand(t *testing.T) {
	command := nodeLocalJailSubmountPermissionsCommand([]string{"/mnt/local-data"})

	assert.Contains(t, command, "for path in '/mnt/local-data'")
	assert.Contains(t, command, `stat -c '%a'`)
	assert.Contains(t, command, `[ "$mode" != "777" ]`)
}

func TestNodeLocalJailSubmountPaths(t *testing.T) {
	mounts := []framework.JailSubMountInfo{
		{Name: "home", MountPath: "/home"},
		{Name: "image-storage", MountPath: "/mnt/image-storage"},
		{Name: "local-data", MountPath: "/mnt/local-data"},
		{Name: "local-nvme", MountPath: "/mnt/local-nvme"},
	}

	paths, err := nodeLocalJailSubmountPaths(mounts, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"/mnt/local-data"}, paths)

	paths, err = nodeLocalJailSubmountPaths(mounts, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"/mnt/local-data", "/mnt/local-nvme"}, paths)
}

func TestNodeLocalJailSubmountPathsAllowsNoLocalData(t *testing.T) {
	paths, err := nodeLocalJailSubmountPaths([]framework.JailSubMountInfo{{Name: "image-storage", MountPath: "/mnt/image-storage"}}, false)
	require.NoError(t, err)
	assert.Empty(t, paths)
}

func TestNodeLocalJailSubmountPathsRequiresConfiguredLocalNVMe(t *testing.T) {
	_, err := nodeLocalJailSubmountPaths(nil, true)
	assert.ErrorContains(t, err, "no local-nvme jail submount")
}

func TestValidateSlurmNodeHealthOutputAllowsHealthyPoweredDownNodes(t *testing.T) {
	output := "NodeName=worker-0 State=IDLE+CLOUD+POWERED_DOWN\nNodeName=worker-1 State=IDLE+POWERING_UP"
	require.NoError(t, validateSlurmNodeHealthOutput(output))
}

func TestValidateSlurmNodeHealthOutputRejectsExactUnhealthyFlags(t *testing.T) {
	err := validateSlurmNodeHealthOutput("NodeName=worker-0 State=IDLE+DRAIN\nNodeName=worker-1 State=DOWN")
	require.Error(t, err)
	assert.ErrorContains(t, err, "worker-0 state=IDLE+DRAIN")
	assert.ErrorContains(t, err, "worker-1 state=DOWN")
}

func TestValidateSlurmNodeHealthOutputRejectsMissingState(t *testing.T) {
	err := validateSlurmNodeHealthOutput("NodeName=worker-0 CPUs=16")
	require.Error(t, err)
	assert.ErrorContains(t, err, "State is missing")
}
