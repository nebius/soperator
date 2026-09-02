package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSlurmNodeInfo(t *testing.T) {
	output := "NodeName=worker-0 InstanceId=compute-instance-1 State=IDLE+DRAIN ThreadsPerCore=1 RealMemory=191356 CfgTRES=cpu=128,mem=1436G,billing=128,gres/gpu=8 Reason=[user_problem] alloc_gpus_busy [root@2026-01-01T00:00:00]"

	info := ParseSlurmNodeInfo("worker-0", output)

	assert.Equal(t, "worker-0", info.Name)
	assert.Equal(t, "compute-instance-1", info.InstanceID)
	assert.Equal(t, "IDLE+DRAIN", info.State)
	assert.Equal(t, "[user_problem] alloc_gpus_busy [root@2026-01-01T00:00:00]", info.Reason)
	assert.Equal(t, uint64(191356), info.RealMemoryMiB)
	assert.Equal(t, 8, info.GPUCount)
	assert.True(t, info.HasStateFlag("DRAIN"))
	assert.True(t, info.HasStateFlag("idle"))
	assert.False(t, info.HasStateFlag("DOWN"))
	assert.True(t, info.ReasonContains("alloc_gpus_busy"))
	assert.False(t, info.IsUsable())
}

func TestParseSlurmNodeInfoGPUCount(t *testing.T) {
	for _, test := range []struct {
		name     string
		cfgTRES  string
		expected int
	}{
		{name: "GB300", cfgTRES: "CfgTRES=cpu=72,mem=2300G,gres/gpu=4", expected: 4},
		{name: "H200", cfgTRES: "CfgTRES=cpu=128,mem=1436G,billing=128,gres/gpu=8", expected: 8},
		{name: "CPU", cfgTRES: "CfgTRES=cpu=32,mem=128G", expected: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := ParseSlurmNodeInfo("worker-0", "NodeName=worker-0 "+test.cfgTRES+" State=IDLE")
			assert.Equal(t, test.expected, info.GPUCount)
		})
	}
}

func TestParseSlurmNodeNames(t *testing.T) {
	assert.Equal(t, []string{"worker-0", "worker-1"}, ParseSlurmNodeNames("\nworker-0\nworker-1\nworker-0\n"))
}
