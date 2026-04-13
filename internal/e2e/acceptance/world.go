package acceptance

import (
	"fmt"
	"math/rand"
	"time"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const commandTimeout = 10 * time.Minute

type world struct {
	logPrefix string

	state *framework.ClusterState
}

func (w *world) AnyWorker() (framework.WorkerRef, error) {
	if len(w.state.Workers) == 0 {
		return framework.WorkerRef{}, fmt.Errorf("no workers discovered")
	}
	return w.state.Workers[rand.Intn(len(w.state.Workers))], nil
}

func (w *world) AnyGPUWorker() (framework.WorkerRef, error) {
	var gpuWorkers []framework.WorkerRef
	for _, worker := range w.state.Workers {
		if worker.HasGPU {
			gpuWorkers = append(gpuWorkers, worker)
		}
	}
	if len(gpuWorkers) == 0 {
		return framework.WorkerRef{}, fmt.Errorf("no GPU workers discovered")
	}
	return gpuWorkers[rand.Intn(len(gpuWorkers))], nil
}
