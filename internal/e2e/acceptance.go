package e2e

import (
	"context"
	"fmt"

	"nebius.ai/slurm-operator/internal/e2e/acceptance"
	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

func RunAcceptance(ctx context.Context, cfg Config) error {
	state := &framework.ClusterState{
		WorkersByNodeSet: make(map[string][]framework.WorkerRef),
	}
	for _, nodeSet := range cfg.Profile.NodeSets {
		state.ExpectedNodeSets = append(state.ExpectedNodeSets, framework.ExpectedNodeSet{
			Name:   nodeSet.Name,
			Size:   nodeSet.Size,
			Preset: nodeSet.Preset,
			HasGPU: parseGPUCount(nodeSet.Preset) > 0,
		})
	}

	runner := acceptance.NewRunner(state)

	if err := runner.Run(ctx); err != nil {
		return fmt.Errorf("run acceptance suite: %w", err)
	}

	return nil
}
