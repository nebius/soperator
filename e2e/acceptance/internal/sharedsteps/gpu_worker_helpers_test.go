package sharedsteps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestPickIdleGPUWorkersForSingleNodeJob(t *testing.T) {
	snapshot := framework.WorkerSnapshot{
		NodeSets: []framework.NodeSetInfo{{Name: "worker", Replicas: 2, HasGPU: true}},
		GPUWorkers: []framework.WorkerInfo{
			{Name: "worker-0", SlurmNode: framework.SlurmNodeInfo{State: "ALLOCATED"}},
			{Name: "worker-1", SlurmNode: framework.SlurmNodeInfo{State: "IDLE", GPUCount: 8}},
		},
	}
	workers, err := pickIdleGPUWorkers(snapshot, 1)
	require.NoError(t, err)
	require.Len(t, workers, 1)
	assert.Equal(t, "worker-1", workers[0].Name)
	assert.Equal(t, 8, workers[0].SlurmNode.GPUCount)
	_, err = pickIdleGPUWorkers(snapshot, 2)
	require.Error(t, err)
	assert.False(t, framework.IsInsufficientWorkers(err))
	_, err = pickIdleGPUWorkers(snapshot, 0)
	require.ErrorContains(t, err, "invalid count")
	_, err = pickIdleGPUWorkers(framework.WorkerSnapshot{}, 1)
	assert.True(t, framework.IsInsufficientWorkers(err))
}

func TestPickIdleGPUWorkers(t *testing.T) {
	snapshot := framework.WorkerSnapshot{
		NodeSets: []framework.NodeSetInfo{{Name: "gpu", Replicas: 3, HasGPU: true}},
		GPUWorkers: []framework.WorkerInfo{
			{Name: "worker-2", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
			{Name: "worker-0", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
			{Name: "worker-1", SlurmNode: framework.SlurmNodeInfo{State: "ALLOCATED"}},
		},
	}

	workers, err := pickIdleGPUWorkers(snapshot, ncclWorkerCount)
	require.NoError(t, err)
	assert.Equal(t, []string{"worker-0", "worker-2"}, framework.WorkerNames(workers))
}

func TestPickIdleGPUWorkersSkipsUnconfiguredCapacity(t *testing.T) {
	snapshot := framework.WorkerSnapshot{
		NodeSets:   []framework.NodeSetInfo{{Name: "gpu", Replicas: 1, HasGPU: true}},
		GPUWorkers: []framework.WorkerInfo{{Name: "worker-0", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}}},
	}

	_, err := pickIdleGPUWorkers(snapshot, ncclWorkerCount)
	require.Error(t, err)
	assert.True(t, framework.IsInsufficientWorkers(err))
}

func TestPickIdleGPUWorkersFailsWhenConfiguredWorkersAreUnavailable(t *testing.T) {
	snapshot := framework.WorkerSnapshot{
		NodeSets: []framework.NodeSetInfo{{Name: "gpu", Replicas: 2, HasGPU: true}},
		GPUWorkers: []framework.WorkerInfo{
			{Name: "worker-0", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
			{Name: "worker-1", SlurmNode: framework.SlurmNodeInfo{State: "POWERED_DOWN"}},
		},
	}

	_, err := pickIdleGPUWorkers(snapshot, ncclWorkerCount)
	require.Error(t, err)
	assert.False(t, framework.IsInsufficientWorkers(err))
	assert.ErrorContains(t, err, "found 1 idle GPU workers, need 2")
	assert.ErrorContains(t, err, "worker-1 state=POWERED_DOWN")
}
