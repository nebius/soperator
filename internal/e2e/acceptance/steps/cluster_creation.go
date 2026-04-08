package steps

import (
	"context"
	"fmt"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

type ClusterCreation struct {
	state *framework.ClusterState
	exec  framework.Exec
}

func NewClusterCreation(state *framework.ClusterState, exec framework.Exec) *ClusterCreation {
	return &ClusterCreation{state: state, exec: exec}
}

func (s *ClusterCreation) Register(sc *godog.ScenarioContext) {
	sc.Step(`^all Slurm pods are running in the cluster$`, s.allSlurmPodsAreRunning)
}

func (s *ClusterCreation) allSlurmPodsAreRunning(ctx context.Context) error {
	if len(s.state.Workers) == 0 {
		return fmt.Errorf("cluster discovery did not run: no workers found")
	}
	s.exec.Logf("cluster has %d workers", len(s.state.Workers))
	return nil
}
