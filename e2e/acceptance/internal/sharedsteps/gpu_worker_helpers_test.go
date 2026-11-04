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
		WorkersByNodeSet: map[string][]framework.WorkerInfo{"worker": {
			{Name: "worker-0", SlurmNode: framework.SlurmNodeInfo{State: "ALLOCATED"}},
			{Name: "worker-1", SlurmNode: framework.SlurmNodeInfo{State: "IDLE", GPUCount: 8}},
		}},
	}
	workers, err := pickIdleGPUWorkers(snapshot, 1, false)
	require.NoError(t, err)
	require.Len(t, workers, 1)
	assert.Equal(t, "worker-1", workers[0].Name)
	assert.Equal(t, 8, workers[0].SlurmNode.GPUCount)
	_, err = pickIdleGPUWorkers(snapshot, 2, false)
	require.Error(t, err)
	assert.False(t, framework.IsInsufficientWorkers(err))
	_, err = pickIdleGPUWorkers(snapshot, 0, false)
	require.ErrorContains(t, err, "invalid count")
	_, err = pickIdleGPUWorkers(framework.WorkerSnapshot{}, 1, false)
	assert.True(t, framework.IsInsufficientWorkers(err))
}

func TestPickIdleGPUWorkers(t *testing.T) {
	snapshot := framework.WorkerSnapshot{
		NodeSets: []framework.NodeSetInfo{{Name: "gpu", Replicas: 3, HasGPU: true}},
		WorkersByNodeSet: map[string][]framework.WorkerInfo{"gpu": {
			{Name: "worker-2", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
			{Name: "worker-0", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
			{Name: "worker-1", SlurmNode: framework.SlurmNodeInfo{State: "ALLOCATED"}},
		}},
	}

	workers, err := pickIdleGPUWorkers(snapshot, ncclWorkerCount, true)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"worker-0", "worker-2"}, framework.WorkerNames(workers))
}

func TestPickIdleGPUWorkersSkipsUnconfiguredCapacity(t *testing.T) {
	snapshot := framework.WorkerSnapshot{
		NodeSets: []framework.NodeSetInfo{{Name: "gpu", Replicas: 1, HasGPU: true}},
	}

	_, err := pickIdleGPUWorkers(snapshot, ncclWorkerCount, true)
	require.Error(t, err)
	assert.True(t, framework.IsInsufficientWorkers(err))
}

func TestPickIdleGPUWorkersFailsWhenConfiguredWorkersAreUnavailable(t *testing.T) {
	snapshot := framework.WorkerSnapshot{
		NodeSets: []framework.NodeSetInfo{{Name: "gpu", Replicas: 2, HasGPU: true}},
		WorkersByNodeSet: map[string][]framework.WorkerInfo{"gpu": {
			{Name: "worker-0", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
			{Name: "worker-1", SlurmNode: framework.SlurmNodeInfo{State: "POWERED_DOWN"}},
		}},
	}

	_, err := pickIdleGPUWorkers(snapshot, ncclWorkerCount, false)
	require.Error(t, err)
	assert.False(t, framework.IsInsufficientWorkers(err))
	assert.ErrorContains(t, err, "found 1 idle GPU workers, need 2")
	assert.ErrorContains(t, err, "worker-1 state=POWERED_DOWN")
}

func TestPickIdleGPUWorkersKeepsNCCLPairInOneNodeSet(t *testing.T) {
	snapshot := framework.WorkerSnapshot{
		NodeSets: []framework.NodeSetInfo{
			{Name: "worker-h100", Replicas: 2, HasGPU: true},
			{Name: "worker-h200", Replicas: 2, HasGPU: true},
		},
		WorkersByNodeSet: map[string][]framework.WorkerInfo{
			"worker-h100": {
				{Name: "worker-h100-0", NodeSetName: "worker-h100", SlurmNode: framework.SlurmNodeInfo{State: "ALLOCATED"}},
				{Name: "worker-h100-1", NodeSetName: "worker-h100", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
			},
			"worker-h200": {
				{Name: "worker-h200-0", NodeSetName: "worker-h200", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
				{Name: "worker-h200-1", NodeSetName: "worker-h200", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
			},
		},
	}

	workers, err := pickIdleGPUWorkers(snapshot, ncclWorkerCount, true)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"worker-h200-0", "worker-h200-1"}, framework.WorkerNames(workers))

	snapshot.WorkersByNodeSet["worker-h200"][0].SlurmNode.State = "IDLE+DRAIN"
	_, err = pickIdleGPUWorkers(snapshot, ncclWorkerCount, true)
	require.Error(t, err)
	assert.False(t, framework.IsInsufficientWorkers(err))
	assert.ErrorContains(t, err, "NodeSet worker-h100: found 1 idle GPU workers, need 2")
	assert.ErrorContains(t, err, "NodeSet worker-h200: found 1 idle GPU workers, need 2")

	workers, err = pickIdleGPUWorkers(snapshot, ncclWorkerCount, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"worker-h100-1", "worker-h200-1"}, framework.WorkerNames(workers))
}

func TestPickIdleGPUWorkersSkipsWhenNoNodeSetHasConfiguredPair(t *testing.T) {
	snapshot := framework.WorkerSnapshot{
		NodeSets: []framework.NodeSetInfo{
			{Name: "worker-h100", Replicas: 1, HasGPU: true},
			{Name: "worker-h200", Replicas: 1, HasGPU: true},
		},
		WorkersByNodeSet: map[string][]framework.WorkerInfo{
			"worker-h100": {{Name: "worker-h100-0", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}}},
			"worker-h200": {{Name: "worker-h200-0", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}}},
		},
	}

	_, err := pickIdleGPUWorkers(snapshot, ncclWorkerCount, true)
	require.Error(t, err)
	assert.True(t, framework.IsInsufficientWorkers(err))
	assert.ErrorContains(t, err, "found 1 GPU workers in one NodeSet, need 2")

	workers, err := pickIdleGPUWorkers(snapshot, ncclWorkerCount, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"worker-h100-0", "worker-h200-0"}, framework.WorkerNames(workers))
}

func TestPickIdleGPUWorkersReportsMissingWorkers(t *testing.T) {
	snapshot := framework.WorkerSnapshot{
		NodeSets: []framework.NodeSetInfo{{Name: "worker-h200", Replicas: 2, HasGPU: true}},
		WorkersByNodeSet: map[string][]framework.WorkerInfo{
			"worker-h200": {{Name: "worker-h200-0", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}}},
		},
	}

	_, err := pickIdleGPUWorkers(snapshot, ncclWorkerCount, true)
	require.Error(t, err)
	assert.False(t, framework.IsInsufficientWorkers(err))
	assert.ErrorContains(t, err, "NodeSet worker-h200: found 1 idle GPU workers, need 2 (2 configured, 1 discovered)")
	assert.ErrorContains(t, err, "1 configured GPU workers missing from Slurm")
}

func TestPickIdleGPUWorkersRandomizesNodeSetsAndWorkers(t *testing.T) {
	snapshot := framework.WorkerSnapshot{
		NodeSets: []framework.NodeSetInfo{
			{Name: "worker-cpu", Replicas: 2},
			{Name: "worker-h100", Replicas: 2, HasGPU: true},
			{Name: "worker-h200", Replicas: 2, HasGPU: true},
		},
		WorkersByNodeSet: map[string][]framework.WorkerInfo{
			"worker-cpu": {
				{Name: "worker-cpu-0", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
				{Name: "worker-cpu-1", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
			},
			"worker-h100": {
				{Name: "worker-h100-0", NodeSetName: "worker-h100", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
				{Name: "worker-h100-1", NodeSetName: "worker-h100", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
			},
			"worker-h200": {
				{Name: "worker-h200-0", NodeSetName: "worker-h200", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
				{Name: "worker-h200-1", NodeSetName: "worker-h200", SlurmNode: framework.SlurmNodeInfo{State: "IDLE"}},
			},
		},
	}

	for _, tc := range []struct {
		count      int
		oneNodeSet bool
	}{
		{count: 1},
		{count: ncclWorkerCount},
		{count: ncclWorkerCount, oneNodeSet: true},
	} {
		seen := make(map[string]bool)
		for range 256 {
			workers, err := pickIdleGPUWorkers(snapshot, tc.count, tc.oneNodeSet)
			require.NoError(t, err)
			require.Len(t, workers, tc.count)
			assert.Contains(t, []string{"worker-h100", "worker-h200"}, workers[0].NodeSetName)
			if tc.oneNodeSet {
				for _, worker := range workers {
					assert.Equal(t, workers[0].NodeSetName, worker.NodeSetName)
				}
			}
			seen[workers[0].Name] = true
		}
		assert.Len(t, seen, 4, "both GPU NodeSets and both worker positions must be selectable with count=%d, oneNodeSet=%t", tc.count, tc.oneNodeSet)
	}
	assert.Equal(t, "worker-h100-0", snapshot.WorkersByNodeSet["worker-h100"][0].Name)
	assert.Equal(t, "worker-h200-0", snapshot.WorkersByNodeSet["worker-h200"][0].Name)
}
