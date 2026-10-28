package sharedsteps

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func pickIdleGPUWorkers(snapshot framework.WorkerSnapshot, count int) ([]framework.WorkerInfo, error) {
	if count < 1 {
		return nil, fmt.Errorf("select idle GPU workers: invalid count %d", count)
	}
	var configured int
	for _, nodeSet := range snapshot.NodeSets {
		if nodeSet.HasGPU && nodeSet.Replicas > 0 {
			configured += nodeSet.Replicas
		}
	}
	if configured < count {
		return nil, &framework.InsufficientWorkersError{
			Label: "GPU workers",
			Found: configured,
			Need:  count,
		}
	}

	var idle []framework.WorkerInfo
	var unavailable []string
	for _, worker := range snapshot.GPUWorkers {
		if worker.SlurmNode.IsReadyForImmediateWork() && worker.SlurmNode.HasStateFlag("IDLE") {
			idle = append(idle, worker)
			continue
		}
		unavailable = append(unavailable, fmt.Sprintf("%s state=%s reason=%s",
			worker.Name, worker.SlurmNode.State, worker.SlurmNode.Reason))
	}
	if len(idle) < count {
		if missing := configured - len(snapshot.GPUWorkers); missing > 0 {
			unavailable = append(unavailable, fmt.Sprintf("%d configured GPU workers missing from Slurm", missing))
		}
		sort.Strings(unavailable)
		return nil, fmt.Errorf("found %d idle GPU workers, need %d (%d configured, %d discovered): %s",
			len(idle), count, configured, len(snapshot.GPUWorkers), strings.Join(unavailable, "; "))
	}

	sort.Slice(idle, func(i, j int) bool {
		return idle[i].Name < idle[j].Name
	})
	return idle[:count], nil
}
