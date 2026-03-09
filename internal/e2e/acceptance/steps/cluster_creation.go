package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

type ClusterCreation struct {
	state *framework.SharedState
	exec  framework.Executor
}

func NewClusterCreation(state *framework.SharedState, exec framework.Executor) ClusterCreation {
	return ClusterCreation{state: state, exec: exec}
}

func (s ClusterCreation) Register(sc *godog.ScenarioContext) {
	sc.Step(`^the provisioned Slurm cluster is reachable$`, s.theProvisionedSlurmClusterIsReachable)
}

func (s ClusterCreation) theProvisionedSlurmClusterIsReachable(ctx context.Context) error {
	if _, err := s.exec.Run(ctx, "kubectl", "get", "pods", "-n", "soperator"); err != nil {
		return err
	}
	if _, err := s.exec.Run(ctx, "kubectl", "get", "pod", "-n", "soperator", "login-0"); err != nil {
		return err
	}
	if _, err := s.exec.Run(ctx, "kubectl", "get", "pod", "-n", "soperator", "controller-0"); err != nil {
		return err
	}

	workerOutput, err := s.exec.ExecController(ctx, `sinfo -hN -o '%N'`)
	if err != nil {
		return fmt.Errorf("discover worker nodes: %w", err)
	}

	var workers []framework.WorkerRef
	for _, line := range strings.Split(workerOutput, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		workers = append(workers, framework.WorkerRef{Name: name})
	}
	if len(workers) == 0 {
		return fmt.Errorf("no worker nodes discovered")
	}
	s.state.Cluster.Workers = workers

	for _, worker := range s.state.Cluster.Workers {
		if _, err := s.exec.ExecController(ctx, fmt.Sprintf("scontrol show node %s", framework.ShellQuote(worker.Name))); err != nil {
			return fmt.Errorf("read slurm worker state for %s: %w", worker.Name, err)
		}
	}

	s.exec.Logf("discovered workers: %s", workerNames(s.state.Cluster.Workers))
	return nil
}

func workerNames(workers []framework.WorkerRef) string {
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		names = append(names, worker.Name)
	}
	return strings.Join(names, ", ")
}
