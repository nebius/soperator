package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSlurmNodeInfo(t *testing.T) {
	output := "NodeName=worker-0 InstanceId=compute-instance-1 State=IDLE+DRAIN ThreadsPerCore=1 RealMemory=191356 Reason=[user_problem] alloc_gpus_busy [root@2026-01-01T00:00:00]"

	info := ParseSlurmNodeInfo("worker-0", output)

	assert.Equal(t, "worker-0", info.Name)
	assert.Equal(t, "compute-instance-1", info.InstanceID)
	assert.Equal(t, "IDLE+DRAIN", info.State)
	assert.Equal(t, "[user_problem] alloc_gpus_busy [root@2026-01-01T00:00:00]", info.Reason)
	assert.Equal(t, uint64(191356), info.RealMemoryMiB)
	assert.True(t, info.HasStateFlag("DRAIN"))
	assert.True(t, info.HasStateFlag("idle"))
	assert.False(t, info.HasStateFlag("DOWN"))
	assert.True(t, info.ReasonContains("alloc_gpus_busy"))
	assert.False(t, info.IsUsable())
}
