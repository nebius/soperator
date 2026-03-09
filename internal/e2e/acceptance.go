package e2e

import (
	"context"
	"fmt"
	"os"

	"nebius.ai/slurm-operator/internal/e2e/acceptance"
)

func RunAcceptance(ctx context.Context, cfg Config) error {
	phase := os.Getenv("E2E_ACCEPTANCE_PHASE")
	runner, err := acceptance.NewRunner(acceptance.Config{
		NebiusProjectID: cfg.Profile.NebiusProjectID,
		ClusterName:     E2EClusterName,
	}, phase)
	if err != nil {
		return fmt.Errorf("configure acceptance runner: %w", err)
	}

	if err := runner.Run(ctx); err != nil {
		return fmt.Errorf("run acceptance suite: %w", err)
	}

	return nil
}
