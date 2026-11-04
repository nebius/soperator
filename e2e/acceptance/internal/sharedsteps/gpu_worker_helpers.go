package sharedsteps

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

// oneNodeSet keeps multi-node GPU jobs on the same GPU type and fabric.
func pickIdleGPUWorkers(snapshot framework.WorkerSnapshot, count int, oneNodeSet bool) ([]framework.WorkerInfo, error) {
	if count < 1 {
		return nil, fmt.Errorf("select idle GPU workers: invalid count %d", count)
	}
	var configured int
	var pools [][]framework.WorkerInfo
	var allWorkers []framework.WorkerInfo
	for _, nodeSet := range snapshot.NodeSets {
		if !nodeSet.HasGPU {
			continue
		}
		workers := snapshot.WorkersByNodeSet[nodeSet.Name]
		if oneNodeSet {
			configured = max(configured, nodeSet.Replicas)
			if nodeSet.Replicas >= count {
				pools = append(pools, workers)
			}
		} else {
			configured += nodeSet.Replicas
			allWorkers = append(allWorkers, workers...)
		}
	}
	if configured < count {
		label := "GPU workers"
		if oneNodeSet {
			label += " in one NodeSet"
		}
		return nil, &framework.InsufficientWorkersError{
			Label: label,
			Found: configured,
			Need:  count,
		}
	}
	if !oneNodeSet {
		pools = append(pools, allWorkers)
	}
	for _, index := range rand.Perm(len(pools)) {
		var idle []framework.WorkerInfo
		for _, worker := range pools[index] {
			if isIdleGPUWorker(worker) {
				idle = append(idle, worker)
			}
		}
		if len(idle) >= count {
			rand.Shuffle(len(idle), func(i, j int) { idle[i], idle[j] = idle[j], idle[i] })
			return idle[:count], nil
		}
	}
	return nil, fmt.Errorf("select %d idle GPU workers (oneNodeSet=%t): %s", count, oneNodeSet, gpuWorkerAvailability(snapshot))
}

func isIdleGPUWorker(worker framework.WorkerInfo) bool {
	return worker.SlurmNode.IsReadyForImmediateWork() && worker.SlurmNode.HasStateFlag("IDLE")
}

func gpuWorkerAvailability(snapshot framework.WorkerSnapshot) string {
	var configured, discovered, totalIdle int
	var details []string
	for _, nodeSet := range snapshot.NodeSets {
		if !nodeSet.HasGPU {
			continue
		}
		workers := snapshot.WorkersByNodeSet[nodeSet.Name]
		var idle int
		var problems []string
		for _, worker := range workers {
			if isIdleGPUWorker(worker) {
				idle++
			} else {
				problems = append(problems, fmt.Sprintf("%s state=%s reason=%s", worker.Name, worker.SlurmNode.State, worker.SlurmNode.Reason))
			}
		}
		if missing := nodeSet.Replicas - len(workers); missing > 0 {
			problems = append(problems, fmt.Sprintf("%d configured GPU workers missing from Slurm", missing))
		}
		if extra := len(workers) - nodeSet.Replicas; extra > 0 {
			problems = append(problems, fmt.Sprintf("%d unexpected GPU workers present in Slurm", extra))
		}
		detail := fmt.Sprintf("NodeSet %s: %d configured, %d discovered, %d idle", nodeSet.Name, nodeSet.Replicas, len(workers), idle)
		if len(problems) > 0 {
			sort.Strings(problems)
			detail += ": " + strings.Join(problems, "; ")
		}
		details = append(details, detail)
		configured += nodeSet.Replicas
		discovered += len(workers)
		totalIdle += idle
	}
	sort.Strings(details)
	return fmt.Sprintf("%d configured, %d discovered, %d idle; %s", configured, discovered, totalIdle, strings.Join(details, "; "))
}
