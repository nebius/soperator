package sharedsteps

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const multiNodeGPUWorkerCount = 2

func (s *PassiveChecks) twoIdleGPUWorkersAreSelected(ctx context.Context) error {
	snapshot, err := s.selector.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("inspect workers for a multi-node GPU job: %w", err)
	}
	// One NodeSet keeps the GPU count, and so "every GPU", the same on both workers.
	var workers []framework.WorkerInfo
	if framework.HasWorkerReservation(ctx) {
		var nodeSet string
		if nodeSet, err = gpuNodeSetWithReplicas(snapshot, multiNodeGPUWorkerCount); err == nil {
			workers, err = s.selector.Acquire(ctx, framework.WorkerRequirements{
				Kind: "GPU", Count: multiNodeGPUWorkerCount, NodeSet: nodeSet, Idle: true,
			})
		}
	} else {
		workers, err = pickIdleGPUWorkers(snapshot, multiNodeGPUWorkerCount, true)
	}
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.multiNodeGPUWorkers = workers
	s.runtime.Logf("passive checks: selected GPU workers for a multi-node job: %s", strings.Join(framework.WorkerNames(workers), ","))
	return nil
}

func (s *PassiveChecks) aSlurmJobUsingEveryGPUOnBothWorkersRuns(ctx context.Context) error {
	if len(s.multiNodeGPUWorkers) != multiNodeGPUWorkerCount {
		return fmt.Errorf("%d GPU workers for a multi-node job are not selected", multiNodeGPUWorkerCount)
	}
	gpus, err := workersGPUCount(s.multiNodeGPUWorkers)
	if err != nil {
		return err
	}
	job, err := s.slurm.SubmitBatch(ctx, framework.SbatchOptions{
		JobName:      "e2e-passive-multinode-gpu-hooks",
		Nodes:        len(s.multiNodeGPUWorkers),
		Nodelist:     framework.WorkerNames(s.multiNodeGPUWorkers),
		GPUsPerNode:  gpus,
		TasksPerNode: 1,
		Args:         []string{"--exclusive"},
		Wrap:         "srun true",
	})
	if err != nil {
		return err
	}
	s.multiNodeGPUJob = job
	return framework.WaitForJobSucceeded(ctx, s.runtime, s.slurm, job, passiveGPUJobTimeout)
}

func (s *PassiveChecks) eachWorkersPrologGPUHealthCheckReportForTheJobIsFreshAndPassing(ctx context.Context) error {
	return s.eachMultiNodeGPUWorkersHealthCheckReportIsFreshAndPassing(ctx, "prolog")
}

func (s *PassiveChecks) eachWorkersEpilogGPUHealthCheckReportForTheJobIsFreshAndPassing(ctx context.Context) error {
	return s.eachMultiNodeGPUWorkersHealthCheckReportIsFreshAndPassing(ctx, "epilog")
}

func (s *PassiveChecks) eachMultiNodeGPUWorkersHealthCheckReportIsFreshAndPassing(ctx context.Context, hook string) error {
	return s.checkEachMultiNodeGPUWorker(func(worker framework.WorkerInfo, jobID string) error {
		targetPath := gpuHealthCheckOutputPath(s.info.TargetSoperatorVersion, worker.Name, hook)
		if _, err := waitForPassingHealthCheckReports(ctx, s.runtime, worker, targetPath, jobID, nil, passiveGPUJobTimeout); err != nil {
			return fmt.Errorf("%s %s report: %w", hook, gpuHealthCheckName, err)
		}
		return nil
	})
}

func (s *PassiveChecks) checkEachMultiNodeGPUWorker(check func(worker framework.WorkerInfo, jobID string) error) error {
	if s.multiNodeGPUJob.IsZero() || len(s.multiNodeGPUWorkers) == 0 {
		return fmt.Errorf("multi-node GPU job is not captured")
	}
	var problems []error
	for _, worker := range s.multiNodeGPUWorkers {
		if err := check(worker, s.multiNodeGPUJob.ID); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", worker.Name, err))
		}
	}
	return errors.Join(problems...)
}

// workersGPUCount returns the GPU count Slurm configured on every worker, which must be the same.
func workersGPUCount(workers []framework.WorkerInfo) (int, error) {
	gpus := 0
	for _, worker := range workers {
		count := worker.SlurmNode.GPUCount
		if count <= 0 {
			return 0, fmt.Errorf("Slurm reports no GPUs on %s", worker.Name)
		}
		if gpus != 0 && count != gpus {
			return 0, fmt.Errorf("selected workers have different GPU counts: %s", formatWorkerGPUCounts(workers))
		}
		gpus = count
	}
	if gpus == 0 {
		return 0, fmt.Errorf("no workers to count GPUs on")
	}
	return gpus, nil
}

func formatWorkerGPUCounts(workers []framework.WorkerInfo) string {
	counts := make([]string, 0, len(workers))
	for _, worker := range workers {
		counts = append(counts, fmt.Sprintf("%s=%d", worker.Name, worker.SlurmNode.GPUCount))
	}
	return strings.Join(counts, ", ")
}

// gpuNodeSetWithReplicas returns the first GPU NodeSet, by name, configured with at least count workers.
func gpuNodeSetWithReplicas(snapshot framework.WorkerSnapshot, count int) (string, error) {
	var names []string
	most := 0
	for _, nodeSet := range snapshot.NodeSets {
		if !nodeSet.HasGPU {
			continue
		}
		most = max(most, nodeSet.Replicas)
		if nodeSet.Replicas >= count {
			names = append(names, nodeSet.Name)
		}
	}
	if len(names) == 0 {
		return "", &framework.InsufficientWorkersError{Label: "GPU workers in one NodeSet", Found: most, Need: count}
	}
	slices.Sort(names)
	return names[0], nil
}
