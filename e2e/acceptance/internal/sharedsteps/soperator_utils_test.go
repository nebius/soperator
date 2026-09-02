package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFSUsageOutput(t *testing.T) {
	types, err := parseFSUsageOutput(`  Size Use% FSType Directory
  100G   2% ext4   /
  1.0T  10% nfs4   /mnt/data
`)
	require.NoError(t, err)
	assert.Equal(t, []string{"ext4", "nfs4"}, types)
}

func TestParseFSUsageOutputAcceptsHeaderOnly(t *testing.T) {
	types, err := parseFSUsageOutput("  Size Use% FSType Directory\n")
	require.NoError(t, err)
	assert.Empty(t, types)
}

func TestParseFSUsageOutputRejectsMalformedOutput(t *testing.T) {
	for name, output := range map[string]string{
		"empty":      "",
		"bad header": "SIZE USE FSTYPE TARGET\n",
		"short row":  "Size Use% FSType Directory\n1G 1% ext4\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseFSUsageOutput(output)
			assert.Error(t, err)
		})
	}
}

func TestValidateFSUsageOutputs(t *testing.T) {
	header := "Size Use% FSType Directory\n"
	outputs := map[string]string{
		"":   header + "100G 2% ext4 /\n1T 10% nfs4 /mnt/data\n",
		"-s": header + "1T 10% nfs4 /mnt/data\n",
		"-l": header + "100G 2% overlay /\n",
		"-m": header,
	}

	assert.NoError(t, validateFSUsageOutputs(outputs))
}

func TestValidateFSUsageOutputsReportsProblemsInModeOrder(t *testing.T) {
	header := "Size Use% FSType Directory\n"
	err := validateFSUsageOutputs(map[string]string{
		"":   header,
		"-s": header + "100G 2% ext4 /\n",
		"-l": header,
	})

	require.EqualError(t, err, `validate fs_usage outputs: none: no filesystems reported; -s: filesystem type "ext4" is not allowed; -m: output is missing`)
}

func TestValidateInstanceLoginOutputs(t *testing.T) {
	assert.NoError(t, validateInstanceLoginOutputs(map[string]string{
		"worker name": "/proc/3914/comm\n",
		"instance ID": "/proc/3914/comm\n",
	}))
}

func TestValidateInstanceLoginOutputsReportsProblemsInOrder(t *testing.T) {
	err := validateInstanceLoginOutputs(map[string]string{
		"worker name": "containerd\n",
	})

	require.EqualError(t, err, `validate instance login outputs: worker name: kubelet process path is missing from "containerd"; instance ID: output is missing`)
}

func TestParseTaskInfo(t *testing.T) {
	info, err := parseTaskInfo("other output\nSLURM_TASK_INFO node=worker-0 rank=0 cpu=2-3 gpu=0000A cuda_dev=0\n")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"node":     "worker-0",
		"rank":     "0",
		"cpu":      "2-3",
		"gpu":      "0000A",
		"cuda_dev": "0",
	}, info)
}

func TestParseTaskInfoRejectsMalformedOutput(t *testing.T) {
	for name, output := range map[string]string{
		"missing":   "unrelated output\n",
		"bad field": "SLURM_TASK_INFO node=worker-0 invalid\n",
		"duplicate": "SLURM_TASK_INFO node=worker-0 node=worker-1\n",
		"two lines": "SLURM_TASK_INFO node=worker-0\nSLURM_TASK_INFO node=worker-1\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTaskInfo(output)
			assert.Error(t, err)
		})
	}
}

func TestValidateTaskInfo(t *testing.T) {
	output := "SLURM_TASK_INFO node=worker-0 rank=0 cpu=2-3 gpu=0000A cuda_dev=0\n"
	assert.NoError(t, validateTaskInfo(output, "worker-0"))
}

func TestValidateTaskInfoReportsRequiredFields(t *testing.T) {
	err := validateTaskInfo("SLURM_TASK_INFO node=worker-1 rank=1 cpu= gpu= cuda_dev=\n", "worker-0")

	require.EqualError(t, err, `validate slurm_task_info output: node: expected "worker-0", got "worker-1"; rank: expected "0", got "1"; cpu: value is empty; gpu: value is empty; cuda_dev: value is empty`)
}
