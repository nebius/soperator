package e2e

import (
	"context"
	"fmt"

	"nebius.ai/slurm-operator/internal/e2e/acceptance"
)

func RunAcceptance(ctx context.Context, cfg Config) error {
	runner := acceptance.NewRunner(acceptance.Config{
		NebiusProjectID: cfg.Profile.NebiusProjectID,
		ClusterName:     E2EClusterName,
	})

	if err := runner.Run(ctx); err != nil {
		return fmt.Errorf("run acceptance suite: %w", err)
	}

	return nil
}
