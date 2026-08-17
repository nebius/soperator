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
		CPUWorkers: []WorkerInfo{{Name: "cpu-0"}},
		GPUWorkers: []WorkerInfo{{Name: "gpu-0"}},
	}

	workers, err := snapshot.PickCPUWorkers(1)
	require.NoError(t, err)
	assert.Equal(t, []WorkerInfo{{Name: "cpu-0"}}, workers)
}

func TestWorkerSnapshotPickGPUWorkersSelectsFromGPUWorkers(t *testing.T) {
	snapshot := WorkerSnapshot{
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
	_, err := (WorkerSnapshot{GPUWorkers: []WorkerInfo{{Name: "gpu-0"}}}).PickGPUWorkers(2)
	require.Error(t, err)
	assert.ErrorContains(t, err, "found 1 GPU workers, need 2")
	assert.True(t, IsInsufficientWorkers(err))
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

func TestClassifyWorkersSeparatesCPUAndGPU(t *testing.T) {
	snapshot := WorkerSnapshot{
		NodeSets: []NodeSetInfo{
			{Name: "worker-gpu", Replicas: 2, HasGPU: true},
			{Name: "worker-cpu", Replicas: 1, HasGPU: false},
		},
		WorkersByNodeSet: map[string][]WorkerInfo{},
	}

	classifyWorkers(&snapshot, []WorkerInfo{
		{Name: "worker-gpu-0"},
		{Name: "worker-cpu-0"},
		{Name: "worker-gpu-1"},
	})

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

type testLogger struct {
	logs []string
}

func (l *testLogger) Logf(format string, args ...any) {
	l.logs = append(l.logs, fmt.Sprintf(format, args...))
}
