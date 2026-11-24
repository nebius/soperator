package sharedsteps

import (
	"fmt"
	"math/rand"
	"slices"
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
	var nodeSets []framework.NodeSetInfo
	var allWorkers []framework.WorkerInfo
	for _, nodeSet := range snapshot.NodeSets {
		if !nodeSet.HasGPU {
			continue
		}
		workers := snapshot.WorkersByNodeSet[nodeSet.Name]
		if oneNodeSet {
			configured = max(configured, nodeSet.Replicas)
			if nodeSet.Replicas >= count {
				nodeSets = append(nodeSets, nodeSet)
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
		workers, err := framework.PickWorkers(allWorkers, configured, count, "GPU workers", framework.WorkerInfo.IsIdle)
		if err == nil {
			return workers, nil
		}
	} else {
		for _, index := range rand.Perm(len(nodeSets)) {
			nodeSet := nodeSets[index]
			workers, err := framework.PickWorkers(snapshot.WorkersByNodeSet[nodeSet.Name], nodeSet.Replicas, count, "GPU workers", framework.WorkerInfo.IsIdle)
			if err == nil {
				return workers, nil
			}
		}
	}
	return nil, fmt.Errorf("select %d idle GPU workers (oneNodeSet=%t): %s", count, oneNodeSet, gpuWorkerAvailability(snapshot, count))
}

func gpuWorkerAvailability(snapshot framework.WorkerSnapshot, count int) string {
	var configured int
	var allWorkers []framework.WorkerInfo
	var details []string
	for _, nodeSet := range snapshot.NodeSets {
		if !nodeSet.HasGPU {
			continue
		}
		workers := snapshot.WorkersByNodeSet[nodeSet.Name]
		details = append(details, fmt.Sprintf("NodeSet %s: %s", nodeSet.Name,
			framework.WorkerAvailability(workers, nodeSet.Replicas, 0, "GPU workers", framework.WorkerInfo.IsIdle)))
		configured += nodeSet.Replicas
		allWorkers = append(allWorkers, workers...)
	}
	sort.Strings(details)
	return framework.WorkerAvailability(allWorkers, configured, count, "GPU workers", framework.WorkerInfo.IsIdle) + "; " + strings.Join(details, "; ")
}

// pickIdleGPUWorkersFromEveryNodeSet takes one idle worker from every GPU
// NodeSet with configured replicas and tops the selection up to minTotal from
// the remaining idle GPU workers. A GPU NodeSet without an idle worker fails
// the selection; a cluster configured with fewer than minTotal GPU workers
// reports insufficient workers.
func pickIdleGPUWorkersFromEveryNodeSet(snapshot framework.WorkerSnapshot, minTotal int) ([]framework.WorkerInfo, error) {
	if minTotal < 1 {
		return nil, fmt.Errorf("select idle GPU workers: invalid minimum %d", minTotal)
	}
	var configured int
	var nodeSets []framework.NodeSetInfo
	for _, nodeSet := range snapshot.NodeSets {
		if nodeSet.HasGPU && nodeSet.Replicas > 0 {
			configured += nodeSet.Replicas
			nodeSets = append(nodeSets, nodeSet)
		}
	}
	if configured < minTotal {
		return nil, &framework.InsufficientWorkersError{Label: "GPU workers", Found: configured, Need: minTotal}
	}
	var selected, spare []framework.WorkerInfo
	for _, nodeSet := range nodeSets {
		pool := snapshot.WorkersByNodeSet[nodeSet.Name]
		workers, err := framework.PickWorkers(pool, nodeSet.Replicas, 1, "GPU workers", framework.WorkerInfo.IsIdle)
		if err != nil {
			return nil, fmt.Errorf("select an idle GPU worker in NodeSet %s: %w", nodeSet.Name, err)
		}
		selected = append(selected, workers[0])
		for _, worker := range pool {
			if worker.Name != workers[0].Name && worker.IsIdle() {
				spare = append(spare, worker)
			}
		}
	}
	for _, index := range rand.Perm(len(spare)) {
		if len(selected) >= minTotal {
			break
		}
		selected = append(selected, spare[index])
	}
	if len(selected) < minTotal {
		return nil, fmt.Errorf("select %d idle GPU workers: %s", minTotal, gpuWorkerAvailability(snapshot, minTotal))
	}
	return selected, nil
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
