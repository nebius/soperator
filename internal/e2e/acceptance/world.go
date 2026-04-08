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
