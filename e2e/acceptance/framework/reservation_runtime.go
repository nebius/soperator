package framework

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

type reservationRuntime struct{ Runtime }

// WithWorkerReservationRuntime scopes raw Slurm clients and utility scripts in
// each jail command. No shared environment is changed between concurrent scenarios.
func WithWorkerReservationRuntime(runtime Runtime) Runtime {
	if runtime == nil {
		return nil
	}
	return &reservationRuntime{Runtime: runtime}
}

func (r *reservationRuntime) Jail() CommandScope {
	return &reservationCommandScope{CommandScope: r.Runtime.Jail()}
}

type reservationCommandScope struct{ CommandScope }

func reservationCommand(ctx context.Context, command string) string {
	r := workerReservation(ctx)
	if r == nil || r.backend == nil {
		return command
	}
	name := ShellQuote(r.id)
	return "export SBATCH_RESERVATION=" + name + " SLURM_RESERVATION=" + name + " SALLOC_RESERVATION=" + name +
		" SBATCH_TIMELIMIT=" + reservationJobTimeLimit + " SLURM_TIMELIMIT=" + reservationJobTimeLimit +
		" SALLOC_TIMELIMIT=" + reservationJobTimeLimit + "; " + command
}

func (s *reservationCommandScope) Run(ctx context.Context, command string) (string, error) {
	return s.CommandScope.Run(ctx, reservationCommand(ctx, command))
}

func (s *reservationCommandScope) RunWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error) {
	return s.CommandScope.RunWithRetry(ctx, reservationCommand(ctx, command), attempts, delay)
}

func (s *reservationCommandScope) RunWithDefaultRetry(ctx context.Context, command string) (string, error) {
	return s.CommandScope.RunWithDefaultRetry(ctx, reservationCommand(ctx, command))
}

func readyWorkerHosts(ctx context.Context, kubectl *KubectlClient) (map[string]bool, error) {
	var nodes corev1.NodeList
	if err := kubectl.GetJSON(ctx, &nodes, "get", "nodes", "-o", "json"); err != nil {
		return nil, err
	}
	ready := make(map[string]bool, len(nodes.Items))
	for _, node := range nodes.Items {
		ready[node.Name] = kubeobjects.NodeReady(node)
	}
	return ready, nil
}
