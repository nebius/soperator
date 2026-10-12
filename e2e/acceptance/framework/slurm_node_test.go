package framework

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSlurmNodeInfo(t *testing.T) {
	output := "NodeName=worker-0 InstanceId=compute-instance-1 State=IDLE+DRAIN ThreadsPerCore=1 RealMemory=191356 CfgTRES=cpu=128,mem=1436G,billing=128,gres/gpu=8 SlurmdStartTime=2026-08-28T10:23:44 Reason=[user_problem] alloc_gpus_busy [root@2026-01-01T00:00:00]"

	info := ParseSlurmNodeInfo("worker-0", output)

	assert.Equal(t, "worker-0", info.Name)
	assert.Equal(t, "compute-instance-1", info.InstanceID)
	assert.Equal(t, "IDLE+DRAIN", info.State)
	assert.Equal(t, "[user_problem] alloc_gpus_busy [root@2026-01-01T00:00:00]", info.Reason)
	assert.Equal(t, uint64(191356), info.RealMemoryMiB)
	assert.Equal(t, 8, info.GPUCount)
	assert.Equal(t, time.Date(2026, time.August, 28, 10, 23, 44, 0, time.UTC), info.SlurmdStartTime)
	assert.True(t, info.HasStateFlag("DRAIN"))
	assert.True(t, info.HasStateFlag("idle"))
	assert.False(t, info.HasStateFlag("DOWN"))
	assert.True(t, info.ReasonContains("alloc_gpus_busy"))
	assert.False(t, info.IsUsable())
}

func TestParseSlurmNodeInfoLeavesInvalidStartTimeUnset(t *testing.T) {
	for _, value := range []string{"None", "invalid"} {
		t.Run(value, func(t *testing.T) {
			info := ParseSlurmNodeInfo("worker-0", "NodeName=worker-0 SlurmdStartTime="+value)
			assert.True(t, info.SlurmdStartTime.IsZero())
		})
	}
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

func TestSlurmClientActiveWorkerStartTimes(t *testing.T) {
	runtime := &slurmNodeRuntime{outputs: map[string]string{
		`sinfo -hN -p main -o '%N'`:     "worker-0\nworker-1\nworker-2\nworker-3\n",
		`scontrol show node 'worker-0'`: "NodeName=worker-0 State=IDLE SlurmdStartTime=2026-08-28T10:23:44",
		`scontrol show node 'worker-1'`: "NodeName=worker-1 State=IDLE+NOT_RESPONDING SlurmdStartTime=2026-08-28T10:23:44",
		`scontrol show node 'worker-2'`: "NodeName=worker-2 State=IDLE+POWERING_DOWN SlurmdStartTime=2026-08-28T10:23:44",
		`scontrol show node 'worker-3'`: "NodeName=worker-3 State=IDLE+POWERED_DOWN SlurmdStartTime=None",
	}}

	startTimes, err := NewSlurmClient(runtime).ActiveWorkerStartTimes(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[string]time.Time{
		"worker-0": time.Date(2026, time.August, 28, 10, 23, 44, 0, time.UTC),
	}, startTimes)
}

func TestSlurmClientActiveWorkerStartTimesRequiresActiveWorker(t *testing.T) {
	runtime := &slurmNodeRuntime{outputs: map[string]string{
		`sinfo -hN -p main -o '%N'`:     "worker-0\n",
		`scontrol show node 'worker-0'`: "NodeName=worker-0 State=IDLE+POWERED_DOWN SlurmdStartTime=None",
	}}

	_, err := NewSlurmClient(runtime).ActiveWorkerStartTimes(t.Context())
	require.Error(t, err)
	assert.ErrorContains(t, err, "find active workers")
}

type slurmNodeRuntime struct {
	Runtime
	outputs map[string]string
}

func (r *slurmNodeRuntime) Controller() CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		output, found := r.outputs[command]
		if !found {
			return "", fmt.Errorf("unexpected controller command: %s", strings.TrimSpace(command))
		}
		return output, nil
	})
}
