package framework

import (
	"errors"
	"fmt"
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkerSnapshotPickWorkersSelectsFromAllWorkers(t *testing.T) {
	snapshot := WorkerSnapshot{
		NodeSets: []NodeSetInfo{
			{Name: "cpu", Replicas: 1},
			{Name: "gpu", Replicas: 1, HasGPU: true},
		},
		Workers: []WorkerInfo{
			{Name: "cpu-0"},
			{Name: "gpu-0"},
		},
	}

	workers, err := snapshot.PickWorkers(2)
	require.NoError(t, err)
	assert.Len(t, workers, 2)
	assert.ElementsMatch(t, snapshot.Workers, workers)
}

func TestWorkerSnapshotPickCPUWorkersSelectsFromCPUWorkers(t *testing.T) {
	snapshot := WorkerSnapshot{
		NodeSets: []NodeSetInfo{
			{Name: "cpu", Replicas: 1},
			{Name: "gpu", Replicas: 1, HasGPU: true},
		},
		CPUWorkers: []WorkerInfo{{Name: "cpu-0"}},
		GPUWorkers: []WorkerInfo{{Name: "gpu-0"}},
	}

	workers, err := snapshot.PickCPUWorkers(1)
	require.NoError(t, err)
	assert.Equal(t, []WorkerInfo{{Name: "cpu-0"}}, workers)
}

func TestWorkerSnapshotPickGPUWorkersSelectsFromGPUWorkers(t *testing.T) {
	snapshot := WorkerSnapshot{
		NodeSets: []NodeSetInfo{
			{Name: "cpu", Replicas: 1},
			{Name: "gpu", Replicas: 1, HasGPU: true},
		},
		CPUWorkers: []WorkerInfo{{Name: "cpu-0"}},
		GPUWorkers: []WorkerInfo{{Name: "gpu-0"}},
	}

	workers, err := snapshot.PickGPUWorkers(1)
	require.NoError(t, err)
	assert.Equal(t, []WorkerInfo{{Name: "gpu-0"}}, workers)
}

func TestWorkerSnapshotPickWorkersRejectsInvalidCount(t *testing.T) {
	_, err := (WorkerSnapshot{}).PickWorkers(0)
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid workers count 0")
}

func TestWorkerSnapshotPickWorkersRejectsInsufficientWorkers(t *testing.T) {
	_, err := (WorkerSnapshot{
		NodeSets:   []NodeSetInfo{{Name: "gpu", Replicas: 1, HasGPU: true}},
		GPUWorkers: []WorkerInfo{{Name: "gpu-0"}},
	}).PickGPUWorkers(2)
	require.Error(t, err)
	assert.ErrorContains(t, err, "found 1 GPU workers, need 2")
	assert.True(t, IsInsufficientWorkers(err))
}

func TestWorkerSnapshotPickWorkersSkipsKindAbsentByDesign(t *testing.T) {
	_, err := (WorkerSnapshot{
		NodeSets:   []NodeSetInfo{{Name: "cpu", Replicas: 2}},
		CPUWorkers: []WorkerInfo{{Name: "cpu-0"}, {Name: "cpu-1"}},
	}).PickGPUWorkers(1)
	require.Error(t, err)
	assert.True(t, IsInsufficientWorkers(err))
	assert.ErrorIs(t, SkipIfInsufficientWorkers(nil, err), godog.ErrSkip)
}

func TestWorkerSnapshotPickWorkersFailsWhenConfiguredWorkerIsMissing(t *testing.T) {
	_, err := (WorkerSnapshot{
		NodeSets:   []NodeSetInfo{{Name: "cpu", Replicas: 1}, {Name: "gpu", Replicas: 1, HasGPU: true}},
		CPUWorkers: []WorkerInfo{{Name: "cpu-0", SlurmNode: SlurmNodeInfo{State: "IDLE"}}},
	}).PickGPUWorkers(1)
	require.Error(t, err)
	assert.False(t, IsInsufficientWorkers(err))
	assert.ErrorContains(t, err, "found 0 usable GPU workers, need 1 (1 configured, 0 discovered)")
	assert.ErrorContains(t, err, "1 configured GPU workers missing from Slurm")
}

func TestWorkerSnapshotPickWorkersFailsWhenConfiguredWorkersAreUnusable(t *testing.T) {
	snapshot := WorkerSnapshot{
		NodeSets: []NodeSetInfo{{Name: "cpu", Replicas: 1}, {Name: "gpu", Replicas: 1, HasGPU: true}},
		CPUWorkers: []WorkerInfo{{
			Name:      "cpu-0",
			SlurmNode: SlurmNodeInfo{State: "IDLE"},
		}},
		GPUWorkers: []WorkerInfo{{
			Name:      "gpu-0",
			SlurmNode: SlurmNodeInfo{State: "IDLE+DRAIN", Reason: "health check failed"},
		}},
	}

	_, err := snapshot.PickGPUWorkers(1)
	require.Error(t, err)
	assert.False(t, IsInsufficientWorkers(err))
	assert.ErrorContains(t, err, "found 0 usable GPU workers, need 1 (1 configured, 1 discovered)")
	assert.ErrorContains(t, err, "gpu-0 state=IDLE+DRAIN reason=health check failed")
}

func TestWorkerSnapshotPickWorkersUsesHealthySubset(t *testing.T) {
	snapshot := WorkerSnapshot{
		NodeSets: []NodeSetInfo{{Name: "worker", Replicas: 2}},
		Workers: []WorkerInfo{
			{Name: "worker-0", SlurmNode: SlurmNodeInfo{State: "IDLE+DRAIN", Reason: "maintenance"}},
			{Name: "worker-1", SlurmNode: SlurmNodeInfo{State: "IDLE"}},
		},
	}

	workers, err := snapshot.PickWorkers(1)
	require.NoError(t, err)
	assert.Equal(t, []WorkerInfo{{Name: "worker-1", SlurmNode: SlurmNodeInfo{State: "IDLE"}}}, workers)
}

func TestWorkerSnapshotRequireGPUWorkersUsableFailsOnPartialDegradation(t *testing.T) {
	snapshot := WorkerSnapshot{
		NodeSets: []NodeSetInfo{{Name: "cpu", Replicas: 1}, {Name: "gpu", Replicas: 2, HasGPU: true}},
		CPUWorkers: []WorkerInfo{{
			Name:      "cpu-0",
			SlurmNode: SlurmNodeInfo{State: "IDLE"},
		}},
		GPUWorkers: []WorkerInfo{
			{Name: "gpu-0", SlurmNode: SlurmNodeInfo{State: "IDLE"}},
			{Name: "gpu-1", SlurmNode: SlurmNodeInfo{State: "DOWN", Reason: "not responding"}},
		},
	}

	err := snapshot.RequireGPUWorkersUsable()
	require.Error(t, err)
	assert.ErrorContains(t, err, "found 1 usable GPU workers, need 2")
	assert.ErrorContains(t, err, "gpu-1 state=DOWN reason=not responding")
}

func TestWorkerSnapshotRequireGPUWorkersUsableReportsUnconfiguredCapacity(t *testing.T) {
	snapshot := WorkerSnapshot{
		NodeSets:   []NodeSetInfo{{Name: "cpu", Replicas: 1}},
		CPUWorkers: []WorkerInfo{{Name: "cpu-0", SlurmNode: SlurmNodeInfo{State: "IDLE"}}},
	}

	err := snapshot.RequireGPUWorkersUsable()
	require.Error(t, err)
	assert.True(t, IsInsufficientWorkers(err))
	assert.ErrorContains(t, err, "found 0 GPU workers, need 1")
}

func TestWorkerSnapshotRequireGPUWorkersUsableRejectsUnexpectedGPUWorkers(t *testing.T) {
	snapshot := WorkerSnapshot{
		NodeSets:   []NodeSetInfo{{Name: "cpu", Replicas: 1}},
		CPUWorkers: []WorkerInfo{{Name: "cpu-0", SlurmNode: SlurmNodeInfo{State: "IDLE"}}},
		GPUWorkers: []WorkerInfo{{Name: "gpu-0", SlurmNode: SlurmNodeInfo{State: "IDLE"}}},
	}

	err := snapshot.RequireGPUWorkersUsable()
	require.Error(t, err)
	assert.False(t, IsInsufficientWorkers(err))
	assert.ErrorContains(t, err, "1 unexpected GPU workers present in Slurm")
}

func TestIsInsufficientWorkersSeesWrappedError(t *testing.T) {
	err := assert.AnError
	assert.False(t, IsInsufficientWorkers(err))
	assert.True(t, IsInsufficientWorkers(errors.Join(&InsufficientWorkersError{Label: "workers", Found: 0, Need: 1})))
}

func TestSkipIfInsufficientWorkers(t *testing.T) {
	logger := &testLogger{}

	assert.NoError(t, SkipIfInsufficientWorkers(logger, nil))
	assert.Same(t, assert.AnError, SkipIfInsufficientWorkers(logger, assert.AnError))

	err := SkipIfInsufficientWorkers(logger, &InsufficientWorkersError{Label: "GPU workers", Found: 0, Need: 1})
	assert.ErrorIs(t, err, godog.ErrSkip)
	assert.Equal(t, []string{"acceptance: found 0 GPU workers, need 1, skipping scenario"}, logger.logs)
}

func TestNewWorkerSnapshotRetainsStateAndDerivesCapacity(t *testing.T) {
	node := SlurmNodeInfo{Name: "worker-gpu-0", State: "IDLE+DRAIN", Reason: "maintenance"}
	snapshot, err := newWorkerSnapshot(
		[]NodeSetInfo{
			{Name: "worker-cpu", Replicas: 2},
			{Name: "worker-gpu", Replicas: 1, HasGPU: true},
		},
		[]WorkerInfo{{Name: node.Name, SlurmNode: node}},
	)
	require.NoError(t, err)

	assert.Equal(t, 3, desiredWorkerCount(snapshot.NodeSets))
	assert.Equal(t, 2, desiredCPUWorkerCount(snapshot.NodeSets))
	assert.Equal(t, 1, desiredGPUWorkerCount(snapshot.NodeSets))
	require.Len(t, snapshot.GPUWorkers, 1)
	assert.Equal(t, node, snapshot.GPUWorkers[0].SlurmNode)
	assert.False(t, snapshot.GPUWorkers[0].IsUsable())
}

func TestWorkerSnapshotHeterogeneousClusterKeepsWorkerKindsIndependent(t *testing.T) {
	snapshot, err := newWorkerSnapshot(
		[]NodeSetInfo{
			{Name: "worker-cpu", Replicas: 2},
			{Name: "worker-gpu", Replicas: 2, HasGPU: true},
		},
		[]WorkerInfo{
			{Name: "worker-cpu-0", SlurmNode: SlurmNodeInfo{State: "IDLE"}},
			{Name: "worker-cpu-1", SlurmNode: SlurmNodeInfo{State: "IDLE"}},
			{Name: "worker-gpu-0", SlurmNode: SlurmNodeInfo{State: "IDLE"}},
			{Name: "worker-gpu-1", SlurmNode: SlurmNodeInfo{State: "DOWN", Reason: "not responding"}},
		},
	)
	require.NoError(t, err)

	cpuWorkers, err := snapshot.PickCPUWorkers(2)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"worker-cpu-0", "worker-cpu-1"}, WorkerNames(cpuWorkers))

	gpuWorkers, err := snapshot.PickGPUWorkers(1)
	require.NoError(t, err)
	assert.Equal(t, []string{"worker-gpu-0"}, WorkerNames(gpuWorkers))

	_, err = snapshot.PickGPUWorkers(2)
	require.Error(t, err)
	assert.False(t, IsInsufficientWorkers(err))
	assert.ErrorContains(t, err, "found 1 usable GPU workers, need 2")
	assert.ErrorContains(t, err, "worker-gpu-1 state=DOWN reason=not responding")

	err = snapshot.RequireAllWorkersUsable()
	require.Error(t, err)
	assert.ErrorContains(t, err, "found 3 usable workers, need 4")
}

func TestClassifyWorkersSeparatesCPUAndGPU(t *testing.T) {
	snapshot := WorkerSnapshot{
		NodeSets: []NodeSetInfo{
			{Name: "worker-gpu", Replicas: 2, HasGPU: true},
			{Name: "worker-cpu", Replicas: 1, HasGPU: false},
		},
		WorkersByNodeSet: map[string][]WorkerInfo{},
	}

	err := classifyWorkers(&snapshot, []WorkerInfo{
		{Name: "worker-gpu-0"},
		{Name: "worker-cpu-0"},
		{Name: "worker-gpu-1"},
	})
	require.NoError(t, err)

	assert.ElementsMatch(t, []WorkerInfo{{Name: "worker-cpu-0", NodeSetName: "worker-cpu"}}, snapshot.CPUWorkers)
	assert.ElementsMatch(t, []WorkerInfo{
		{Name: "worker-gpu-0", NodeSetName: "worker-gpu", HasGPU: true},
		{Name: "worker-gpu-1", NodeSetName: "worker-gpu", HasGPU: true},
	}, snapshot.GPUWorkers)
	assert.ElementsMatch(t, []WorkerInfo{{Name: "worker-cpu-0", NodeSetName: "worker-cpu"}}, snapshot.WorkersByNodeSet["worker-cpu"])
	assert.ElementsMatch(t, []WorkerInfo{
		{Name: "worker-gpu-0", NodeSetName: "worker-gpu", HasGPU: true},
		{Name: "worker-gpu-1", NodeSetName: "worker-gpu", HasGPU: true},
	}, snapshot.WorkersByNodeSet["worker-gpu"])
}

func TestClassifyWorkersRejectsUnmatchedWorkers(t *testing.T) {
	snapshot := WorkerSnapshot{
		NodeSets: []NodeSetInfo{
			{Name: "worker-gpu", HasGPU: true},
			{Name: "worker-cpu"},
		},
		WorkersByNodeSet: map[string][]WorkerInfo{},
	}

	err := classifyWorkers(&snapshot, []WorkerInfo{{Name: "other-0"}})
	require.Error(t, err)
	assert.EqualError(t, err,
		"workers do not match any NodeSet: other-0; known NodeSets: worker-cpu, worker-gpu")
}

func TestClassifyWorkersUsesLongestNodeSetPrefix(t *testing.T) {
	snapshot := WorkerSnapshot{
		NodeSets: []NodeSetInfo{
			{Name: "worker", HasGPU: false},
			{Name: "worker-gpu", HasGPU: true},
		},
		WorkersByNodeSet: map[string][]WorkerInfo{},
	}

	err := classifyWorkers(&snapshot, []WorkerInfo{{Name: "worker-gpu-0"}})
	require.NoError(t, err)
	require.Len(t, snapshot.GPUWorkers, 1)
	assert.Equal(t, "worker-gpu", snapshot.GPUWorkers[0].NodeSetName)
}

type testLogger struct {
	logs []string
}

func (l *testLogger) Logf(format string, args ...any) {
	l.logs = append(l.logs, fmt.Sprintf(format, args...))
}

func TestWorkerSnapshotPickWorkersByType(t *testing.T) {
	snapshot := dockerWorkerSnapshotForTest(t).WithDocker()
	for workerType, wantGPU := range map[string]bool{"CPU": false, "cpu": false, "GPU": true, "gpu": true} {
		t.Run(workerType, func(t *testing.T) {
			workers, err := snapshot.PickWorkersByType(workerType, 2)
			require.NoError(t, err)
			require.Len(t, workers, 2)
			for _, worker := range workers {
				assert.Equal(t, wantGPU, worker.HasGPU)
			}
		})
	}
	_, err := snapshot.PickWorkersByType("TPU", 1)
	require.Error(t, err)
	assert.ErrorContains(t, err, `unsupported worker type "TPU"`)
	assert.False(t, IsInsufficientWorkers(err))
}

func TestWorkerSnapshotWithDocker(t *testing.T) {
	for _, workerType := range []string{"CPU", "GPU"} {
		t.Run(workerType, func(t *testing.T) {
			snapshot := dockerWorkerSnapshotForTest(t)
			snapshot = snapshot.WithDocker()
			workers, err := snapshot.PickWorkersByType(workerType, 2)
			require.NoError(t, err)
			for _, worker := range workers {
				assert.Equal(t, workerType == "GPU", worker.HasGPU)
				assert.NotContains(t, worker.NodeSetName, "disabled")
			}
			assert.Len(t, workers, 2)
			_, err = snapshot.PickWorkersByType(workerType, 3)
			assert.True(t, IsInsufficientWorkers(err))
		})
	}
}

func TestWorkerSnapshotWithDockerSkipsUnconfiguredCapacity(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*WorkerSnapshot)
	}{
		{name: "absent kind", prepare: func(snapshot *WorkerSnapshot) {
			snapshot.NodeSets = snapshot.NodeSets[1:]
		}},
		{name: "Docker disabled", prepare: func(snapshot *WorkerSnapshot) {
			snapshot.NodeSets[0].DockerEnabled = false
		}},
		{name: "too few configured", prepare: func(snapshot *WorkerSnapshot) {
			snapshot.NodeSets[0].Replicas = 1
			snapshot.WorkersByNodeSet["cpu"] = snapshot.WorkersByNodeSet["cpu"][:1]
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := dockerWorkerSnapshotForTest(t)
			test.prepare(&snapshot)
			_, err := snapshot.WithDocker().PickCPUWorkers(2)
			require.Error(t, err)
			assert.ErrorIs(t, SkipIfInsufficientWorkers(nil, err), godog.ErrSkip)
		})
	}
	_, err := (WorkerSnapshot{}).WithDocker().PickGPUWorkers(1)
	assert.ErrorIs(t, SkipIfInsufficientWorkers(nil, err), godog.ErrSkip)
}

func TestWorkerSnapshotWithDockerFailsWhenConfiguredWorkersAreUnavailable(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*WorkerSnapshot)
	}{
		{name: "missing", prepare: func(snapshot *WorkerSnapshot) {
			snapshot.WorkersByNodeSet["gpu"] = snapshot.WorkersByNodeSet["gpu"][:1]
		}},
		{name: "unhealthy", prepare: func(snapshot *WorkerSnapshot) {
			snapshot.WorkersByNodeSet["gpu"][1].SlurmNode.State = "IDLE+DRAIN"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := dockerWorkerSnapshotForTest(t)
			test.prepare(&snapshot)
			_, err := snapshot.WithDocker().PickGPUWorkers(2)
			require.Error(t, err)
			assert.False(t, IsInsufficientWorkers(err))
			assert.ErrorContains(t, err, "found 1 usable GPU workers, need 2")
			_, err = snapshot.WithDocker().PickCPUWorkers(2)
			assert.NoError(t, err)
		})
	}
}

func dockerWorkerSnapshotForTest(t *testing.T) WorkerSnapshot {
	t.Helper()
	nodeSets := []NodeSetInfo{
		{Name: "cpu", Replicas: 2, DockerEnabled: true},
		{Name: "gpu", Replicas: 2, DockerEnabled: true, HasGPU: true},
		{Name: "disabled", Replicas: 2},
		{Name: "disabled-gpu", Replicas: 2, HasGPU: true},
	}
	var workers []WorkerInfo
	for _, nodeSet := range nodeSets {
		for _, suffix := range []string{"-0", "-1"} {
			workers = append(workers, WorkerInfo{
				Name:      nodeSet.Name + suffix,
				SlurmNode: SlurmNodeInfo{State: "IDLE"},
			})
		}
	}
	snapshot, err := newWorkerSnapshot(nodeSets, workers)
	require.NoError(t, err)
	return snapshot
}

func TestWorkerSnapshotWithDockerPreservesIndexes(t *testing.T) {
	original := dockerWorkerSnapshotForTest(t)
	filtered := original.WithDocker()
	require.Len(t, filtered.NodeSets, 2)
	require.Len(t, filtered.WorkersByNodeSet, 2)
	assert.ElementsMatch(t, []string{"cpu-0", "cpu-1", "gpu-0", "gpu-1"}, WorkerNames(filtered.Workers))
	assert.ElementsMatch(t, []string{"cpu-0", "cpu-1"}, WorkerNames(filtered.CPUWorkers))
	assert.ElementsMatch(t, []string{"gpu-0", "gpu-1"}, WorkerNames(filtered.GPUWorkers))
	assert.ElementsMatch(t, []string{"cpu-0", "cpu-1"}, WorkerNames(filtered.WorkersByNodeSet["cpu"]))
	assert.ElementsMatch(t, []string{"gpu-0", "gpu-1"}, WorkerNames(filtered.WorkersByNodeSet["gpu"]))
	workers, err := filtered.PickWorkers(4)
	require.NoError(t, err)
	assert.Len(t, workers, 4)
	assert.Len(t, original.NodeSets, 4)
	assert.Len(t, original.Workers, 8)
	assert.Len(t, original.WorkersByNodeSet, 4)
}
