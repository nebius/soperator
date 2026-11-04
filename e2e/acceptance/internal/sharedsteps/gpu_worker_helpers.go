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
	var candidates [][]framework.WorkerInfo
	var allIdle []framework.WorkerInfo
	var unavailable []string
	for _, nodeSet := range snapshot.NodeSets {
		if !nodeSet.HasGPU {
			continue
		}
		if oneNodeSet {
			configured = max(configured, nodeSet.Replicas)
			if nodeSet.Replicas < count {
				continue
			}
		} else {
			configured += nodeSet.Replicas
		}

		workers := snapshot.WorkersByNodeSet[nodeSet.Name]
		var idle []framework.WorkerInfo
		var states []string
		for _, worker := range workers {
			if worker.SlurmNode.IsReadyForImmediateWork() && worker.SlurmNode.HasStateFlag("IDLE") {
				idle = append(idle, worker)
				continue
			}
			states = append(states, fmt.Sprintf("%s state=%s reason=%s",
				worker.Name, worker.SlurmNode.State, worker.SlurmNode.Reason))
		}
		if !oneNodeSet {
			allIdle = append(allIdle, idle...)
		}
		if len(idle) >= count {
			if oneNodeSet {
				candidates = append(candidates, idle)
			}
			continue
		}
		if missing := nodeSet.Replicas - len(workers); missing > 0 {
			states = append(states, fmt.Sprintf("%d configured GPU workers missing from Slurm", missing))
		}
		sort.Strings(states)
		unavailable = append(unavailable, fmt.Sprintf("NodeSet %s: found %d idle GPU workers, need %d (%d configured, %d discovered): %s",
			nodeSet.Name, len(idle), count, nodeSet.Replicas, len(workers), strings.Join(states, "; ")))
	}
	label := "GPU workers"
	if oneNodeSet {
		label += " in one NodeSet"
	}
	if configured < count {
		return nil, &framework.InsufficientWorkersError{
			Label: label,
			Found: configured,
			Need:  count,
		}
	}

	if !oneNodeSet && len(allIdle) >= count {
		candidates = [][]framework.WorkerInfo{allIdle}
	}
	if len(candidates) == 0 {
		sort.Strings(unavailable)
		return nil, fmt.Errorf("select %d idle %s: %s", count, label, strings.Join(unavailable, "; "))
	}

	idle := candidates[rand.Intn(len(candidates))]
	workers := make([]framework.WorkerInfo, 0, count)
	for _, index := range rand.Perm(len(idle))[:count] {
		workers = append(workers, idle[index])
	}
	return workers, nil
}
