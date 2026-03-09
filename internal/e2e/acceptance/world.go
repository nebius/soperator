package acceptance

import (
	"fmt"
	"math/rand"
	"time"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

type world struct {
	cfg              Config
	commandTimeout   time.Duration
	pollInterval     time.Duration
	replacementDelay time.Duration
	logPrefix        string

	state *framework.SharedState
}

func (w *world) AnyWorker() (framework.WorkerRef, error) {
	if len(w.state.Cluster.Workers) == 0 {
		return framework.WorkerRef{}, fmt.Errorf("no workers discovered")
	}
	return w.state.Cluster.Workers[rand.Intn(len(w.state.Cluster.Workers))], nil
}
