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
