package sharedsteps

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskPrologNetworkEnvCommand(t *testing.T) {
	command := taskPrologNetworkEnvCommand("worker-gpu-0", 30*time.Second, 90*time.Second)

	assert.Contains(t, command, "timeout 90 srun")
	assert.Contains(t, command, "--immediate=30")
	assert.Contains(t, command, "-w 'worker-gpu-0'")
	assert.Contains(t, command, "--gpus-per-node=1")
	assert.Contains(t, command, "UCX_NET_DEVICES")
	assert.Contains(t, command, "NCCL_IB_HCA")
}

func TestParseTaskPrologEnvOutput(t *testing.T) {
	vars, err := parseTaskPrologEnvOutput(`cpu-bind: worker-gpu-0
UCX_NET_DEVICES=mlx5_4:1,mlx5_5:1
NCCL_IB_HCA=mlx5_4:1,mlx5_5:1
`)

	require.NoError(t, err)
	assert.Equal(t, "mlx5_4:1,mlx5_5:1", vars[ucxNetDevicesName])
	assert.Equal(t, "mlx5_4:1,mlx5_5:1", vars[ncclIBHCAName])
}

func TestParseTaskPrologEnvOutputRejectsDuplicates(t *testing.T) {
	_, err := parseTaskPrologEnvOutput(`UCX_NET_DEVICES=mlx5_4:1
UCX_NET_DEVICES=mlx5_5:1
`)

	require.Error(t, err)
	assert.ErrorContains(t, err, "parse duplicate UCX_NET_DEVICES")
}

func TestValidateTaskPrologNetworkEnv(t *testing.T) {
	vars := map[string]string{
		ucxNetDevicesName: "mlx5_4:1,mlx5_5:1",
		ncclIBHCAName:     "mlx5_4:1,mlx5_5:1",
	}

	assert.NoError(t, validateTaskPrologNetworkEnv(vars))
}

func TestValidateTaskPrologNetworkEnvReportsMissingAndInvalidValues(t *testing.T) {
	vars := map[string]string{
		ucxNetDevicesName: "eth0",
	}

	err := validateTaskPrologNetworkEnv(vars)
	require.Error(t, err)
	assert.ErrorContains(t, err, `UCX_NET_DEVICES="eth0" does not match mlx5 device list format`)
	assert.ErrorContains(t, err, "NCCL_IB_HCA is missing")
}
