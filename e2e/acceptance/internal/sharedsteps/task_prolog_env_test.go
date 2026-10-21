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
	assert.Contains(t, command, "bash -c")
	assert.NotContains(t, command, "bash -lc")
	assert.Contains(t, command, "UCX_NET_DEVICES")
	assert.Contains(t, command, "NCCL_IB_HCA")
	assert.Contains(t, command, taskPrologNCCLNetworkVarsPath)
}

func TestTaskPrologWorkerSSHNetworkEnvCommand(t *testing.T) {
	command := taskPrologWorkerSSHNetworkEnvCommand("bob", "worker-gpu-0", 30*time.Second)

	assert.Contains(t, command, "su - 'bob' -c")
	assert.Contains(t, command, "timeout 30 ssh")
	assert.Contains(t, command, "BatchMode=yes")
	assert.Contains(t, command, "'worker-gpu-0'")
	assert.Contains(t, command, "bash -lc")
	assert.Contains(t, command, "UCX_NET_DEVICES")
	assert.Contains(t, command, "NCCL_IB_HCA")
	assert.Contains(t, command, taskPrologNCCLNetworkVarsPath)
}

func TestParseTaskPrologEnvOutput(t *testing.T) {
	vars, err := parseTaskPrologEnvOutput(`cpu-bind: worker-gpu-0
ACTUAL_UCX_NET_DEVICES=mlx5_4:1,mlx5_5:1
ACTUAL_NCCL_IB_HCA=mlx5_4:1,mlx5_5:1
EXPECTED_UCX_NET_DEVICES=mlx5_4:1,mlx5_5:1
EXPECTED_NCCL_IB_HCA=mlx5_4:1,mlx5_5:1
`)

	require.NoError(t, err)
	assert.Equal(t, "mlx5_4:1,mlx5_5:1", vars[actualUCXNetDevicesName])
	assert.Equal(t, "mlx5_4:1,mlx5_5:1", vars[actualNCCLIBHCAName])
	assert.Equal(t, "mlx5_4:1,mlx5_5:1", vars[expectedUCXNetDevicesName])
	assert.Equal(t, "mlx5_4:1,mlx5_5:1", vars[expectedNCCLIBHCAName])
}

func TestParseTaskPrologEnvOutputRejectsDuplicates(t *testing.T) {
	_, err := parseTaskPrologEnvOutput(`ACTUAL_UCX_NET_DEVICES=mlx5_4:1
ACTUAL_UCX_NET_DEVICES=mlx5_5:1
`)

	require.Error(t, err)
	assert.ErrorContains(t, err, "parse duplicate ACTUAL_UCX_NET_DEVICES")
}

func TestValidateTaskPrologNetworkEnv(t *testing.T) {
	vars := map[string]string{
		actualUCXNetDevicesName:   "^mlx5_4,mlx5_5",
		actualNCCLIBHCAName:       "=mlx5_4,mlx5_5",
		expectedUCXNetDevicesName: "^mlx5_4,mlx5_5",
		expectedNCCLIBHCAName:     "=mlx5_4,mlx5_5",
	}

	assert.NoError(t, validateTaskPrologNetworkEnv(vars, "job environment"))
}

func TestValidateTaskPrologNetworkEnvReportsMissingAndMismatchedValues(t *testing.T) {
	vars := map[string]string{
		actualUCXNetDevicesName:   "mlx5_0",
		expectedUCXNetDevicesName: "mlx5_1",
		expectedNCCLIBHCAName:     "mlx5_4",
	}

	err := validateTaskPrologNetworkEnv(vars, "worker SSH login environment")
	require.Error(t, err)
	assert.ErrorContains(t, err, `UCX_NET_DEVICES="mlx5_0" in worker SSH login environment, want "mlx5_1"`)
	assert.ErrorContains(t, err, "NCCL_IB_HCA is missing from worker SSH login environment output")
}
