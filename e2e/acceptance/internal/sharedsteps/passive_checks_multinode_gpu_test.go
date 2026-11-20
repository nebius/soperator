package sharedsteps

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func multiNodeTestWorkers(gpus ...int) []framework.WorkerInfo {
	workers := make([]framework.WorkerInfo, 0, len(gpus))
	for i, count := range gpus {
		workers = append(workers, framework.WorkerInfo{
			Name:      "worker-" + string(rune('0'+i)),
			HasGPU:    true,
			SlurmNode: framework.SlurmNodeInfo{Name: "worker-" + string(rune('0'+i)), State: "IDLE", GPUCount: count},
		})
	}
	return workers
}

func TestWorkersGPUCount(t *testing.T) {
	gpus, err := workersGPUCount(multiNodeTestWorkers(8, 8))
	require.NoError(t, err)
	assert.Equal(t, 8, gpus)

	_, err = workersGPUCount(multiNodeTestWorkers(8, 4))
	require.ErrorContains(t, err, "different GPU counts: worker-0=8, worker-1=4")
	_, err = workersGPUCount(multiNodeTestWorkers(8, 0))
	require.ErrorContains(t, err, "no GPUs on worker-1")
	_, err = workersGPUCount(nil)
	require.Error(t, err)
}

func TestFullNodeMultiNodeGPUJobRequestsEveryGPUOnBothWorkers(t *testing.T) {
	var sbatch string
	gone := false
	runtime := newRecordingRuntime(t, func(command string) (string, error) {
		switch {
		case strings.Contains(command, "sbatch "):
			sbatch = command
			return "77\n", nil
		case strings.HasPrefix(command, "squeue"):
			gone = true
			return "", nil
		case strings.HasPrefix(command, "sacct"):
			return "77|COMPLETED|0:0|None|2026-11-17T13:00:00|2026-11-17T13:00:05\n", nil
		default:
			return "", nil
		}
	})
	steps := recordingPassiveChecks(runtime)
	steps.multiNodeGPUWorkers = multiNodeTestWorkers(8, 8)

	require.NoError(t, steps.aSlurmJobUsingEveryGPUOnBothWorkersRuns(t.Context()))
	assert.True(t, gone)
	assert.Equal(t, "77", steps.multiNodeGPUJob.ID)
	for _, part := range []string{"-N 2", "--nodelist='worker-0,worker-1'", "--gpus-per-node=8", "'--exclusive'", "--wrap='srun true'"} {
		assert.Contains(t, sbatch, part)
	}
}

func TestMultiNodeGPUJobChecksEveryWorkerForTheJob(t *testing.T) {
	report := func(worker, jobID string) string {
		return `{"status":"PASS","meta":{"run_id":"run-` + worker + `","environment":"SLURMD_NODENAME=` + worker + ` SLURM_JOB_ID=` + jobID + `"}}`
	}
	workerScope := func(name string, reportJobID string) framework.CommandScope {
		return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
			switch {
			case strings.Contains(command, name+".gpu_health_check.prolog.out"), strings.Contains(command, name+".gpu_health_check.epilog.out"):
				return report(name, reportJobID), nil
			default:
				t.Fatalf("unexpected command on %s: %s", name, command)
				return "", nil
			}
		})
	}
	runtime := &periodicGPURuntime{workers: map[string]framework.CommandScope{
		"worker-0": workerScope("worker-0", "77"),
		"worker-1": workerScope("worker-1", "76"),
	}}
	steps := PassiveChecks{
		info:                &framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"},
		runtime:             runtime,
		multiNodeGPUWorkers: multiNodeTestWorkers(8, 8),
		multiNodeGPUJob:     framework.SbatchJob{ID: "77"},
	}

	err := steps.eachWorkersPrologGPUHealthCheckReportForTheJobIsFreshAndPassing(t.Context())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "worker-0:")
	assert.Contains(t, err.Error(), "worker-1: prolog gpu_health_check report")

	steps.multiNodeGPUJob = framework.SbatchJob{}
	require.ErrorContains(t, steps.eachWorkersEpilogGPUHealthCheckReportForTheJobIsFreshAndPassing(t.Context()), "multi-node GPU job is not captured")
}

func TestGPUNodeSetWithReplicas(t *testing.T) {
	snapshot := framework.WorkerSnapshot{NodeSets: []framework.NodeSetInfo{
		{Name: "cpu", Replicas: 4},
		{Name: "gpu-small", HasGPU: true, Replicas: 1},
		{Name: "gpu-b", HasGPU: true, Replicas: 2},
		{Name: "gpu-a", HasGPU: true, Replicas: 3},
	}}
	nodeSet, err := gpuNodeSetWithReplicas(snapshot, 2)
	require.NoError(t, err)
	assert.Equal(t, "gpu-a", nodeSet)

	_, err = gpuNodeSetWithReplicas(snapshot, 4)
	require.True(t, framework.IsInsufficientWorkers(err), "a cluster without a large enough GPU NodeSet skips the scenario")
}
