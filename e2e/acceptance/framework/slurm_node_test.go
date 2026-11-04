package framework

import (
	"context"
	"errors"
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

func TestParseSlurmNodeAllocations(t *testing.T) {
	for _, tc := range []struct {
		name        string
		output      string
		cpu, memory *uint64
	}{
		{"idle", "CPUAlloc=0 AllocMem=0", new(uint64(0)), new(uint64(0))},
		{"allocated", "CPUAlloc=2 AllocMem=128", new(uint64(2)), new(uint64(128))},
		{"missing", "State=IDLE", nil, nil},
		{"invalid", "CPUAlloc=N/A AllocMem=0bad", nil, nil},
		{"negative", "CPUAlloc=-1 AllocMem=-1", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := ParseSlurmNodeInfo("worker-0", tc.output)
			assert.Equal(t, tc.cpu, info.CPUAlloc)
			assert.Equal(t, tc.memory, info.AllocMemoryMiB)
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

func TestSlurmNodeHealthAndImmediateReadiness(t *testing.T) {
	tests := []struct {
		state          string
		healthy        bool
		immediateReady bool
	}{
		{state: "IDLE", healthy: true, immediateReady: true},
		{state: "ALLOCATED+CLOUD", healthy: true, immediateReady: true},
		{state: "IDLE+POWERED_DOWN", healthy: true, immediateReady: false},
		{state: "IDLE+POWERING_UP", healthy: true, immediateReady: false},
		{state: "IDLE+POWERING_DOWN", healthy: true, immediateReady: false},
		{state: "IDLE+POWER_DOWN", healthy: true, immediateReady: false},
		{state: "IDLE+DRAIN", healthy: false, immediateReady: false},
		{state: "DOWN", healthy: false, immediateReady: false},
		{state: "IDLE+NOT_RESPONDING", healthy: false, immediateReady: false},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			node := SlurmNodeInfo{State: tt.state}
			assert.Equal(t, tt.healthy, node.IsHealthy())
			assert.Equal(t, tt.healthy, node.IsUsable())
			assert.Equal(t, tt.immediateReady, node.IsReadyForImmediateWork())
		})
	}
}

func TestSlurmNodeInfoIsUsableReplacement(t *testing.T) {
	for _, tc := range []struct {
		name       string
		instanceID string
		state      string
		want       bool
	}{
		{name: "missing instance", state: "IDLE"},
		{name: "original instance", instanceID: "old-instance", state: "IDLE"},
		{name: "missing state", instanceID: "new-instance"},
		{name: "drained", instanceID: "new-instance", state: "IDLE+DRAIN"},
		{name: "down", instanceID: "new-instance", state: "DOWN"},
		{name: "failed", instanceID: "new-instance", state: "FAIL"},
		{name: "not responding", instanceID: "new-instance", state: "IDLE+NOT_RESPONDING"},
		{name: "invalid registration", instanceID: "new-instance", state: "IDLE+INVALID_REG"},
		{name: "idle", instanceID: "new-instance", state: "IDLE", want: true},
		{name: "allocated", instanceID: "new-instance", state: "ALLOCATED", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := SlurmNodeInfo{InstanceID: tc.instanceID, State: tc.state}
			assert.Equal(t, tc.want, node.IsUsableReplacement("old-instance"))
		})
	}
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

func TestSlurmClientNodeInfosOnceParsesAndOrdersSelectedRecords(t *testing.T) {
	workerOne := "NodeName=worker-1 InstanceId=instance-1 State=ALLOCATED+DRAIN+REBOOT_REQUESTED CPUAlloc=2 AllocMem=128 Reason=soperator rolling update : test reason"
	workerZero := "NodeName=worker-0 InstanceId=instance-0 State=IDLE CPUAlloc=0 AllocMem=0"
	runtime := &slurmNodeRuntime{outputs: map[string]string{
		"scontrol -o show nodes": "\nNodeName=unselected\n" + workerOne + "\n" + workerZero + "\n",
	}}

	nodes, err := NewSlurmClient(runtime).NodeInfosOnce(t.Context(), []string{"worker-0", "worker-1"})

	require.NoError(t, err)
	assert.Equal(t, []SlurmNodeInfo{ParseSlurmNodeInfo("worker-0", workerZero), ParseSlurmNodeInfo("worker-1", workerOne)}, nodes)
	assert.Equal(t, []string{"scontrol -o show nodes"}, runtime.calls)
}

func TestSlurmClientNodeInfosOnceValidatesInputsAndRecords(t *testing.T) {
	for _, test := range []struct {
		name      string
		names     []string
		output    string
		wantError string
		wantCalls int
	}{
		{name: "empty"},
		{name: "empty name", names: []string{""}, wantError: "invalid or duplicate node name"},
		{name: "hostlist", names: []string{"worker-[0-1]"}, wantError: "invalid or duplicate node name"},
		{name: "duplicate input", names: []string{"worker-0", "worker-0"}, wantError: "invalid or duplicate node name"},
		{name: "empty output", names: []string{"worker-0"}, wantError: "missing record for worker-0", wantCalls: 1},
		{name: "missing selected record", names: []string{"worker-0", "worker-1"}, output: "NodeName=worker-0 State=IDLE", wantError: "missing record for worker-1", wantCalls: 1},
		{name: "duplicate selected record", names: []string{"worker-0"}, output: "NodeName=worker-0 State=IDLE\nNodeName=worker-0 State=DOWN", wantError: "duplicate record for worker-0", wantCalls: 1},
		{name: "malformed leading field", names: []string{"worker-0"}, output: "State=IDLE NodeName=worker-0", wantError: "invalid leading field", wantCalls: 1},
		{name: "invalid record name", names: []string{"worker-0"}, output: "NodeName=worker-[0-1] State=IDLE", wantError: "invalid leading field", wantCalls: 1},
		{name: "missing selected state", names: []string{"worker-0"}, output: "NodeName=worker-0 CPUAlloc=0", wantError: "missing state", wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := &slurmNodeRuntime{outputs: map[string]string{"scontrol -o show nodes": test.output}}

			nodes, err := NewSlurmClient(runtime).NodeInfosOnce(t.Context(), test.names)

			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
			}
			assert.Empty(t, nodes)
			assert.Len(t, runtime.calls, test.wantCalls)
		})
	}
}

func TestSlurmClientNodeInfosOnceReturnsCommandErrorWithoutRetry(t *testing.T) {
	commandErr := errors.New("controller unavailable")
	runtime := &slurmNodeRuntime{runError: commandErr}

	_, err := NewSlurmClient(runtime).NodeInfosOnce(t.Context(), []string{"worker-0"})

	require.ErrorIs(t, err, commandErr)
	assert.Equal(t, []string{"scontrol -o show nodes"}, runtime.calls)
}

type slurmNodeRuntime struct {
	Runtime
	outputs  map[string]string
	calls    []string
	runError error
}

func (r *slurmNodeRuntime) Controller() CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		r.calls = append(r.calls, command)
		if r.runError != nil {
			return "", r.runError
		}
		output, found := r.outputs[command]
		if !found {
			return "", fmt.Errorf("unexpected controller command: %s", strings.TrimSpace(command))
		}
		return output, nil
	})
}
