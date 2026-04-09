package e2e

import (
	"context"
	"fmt"

	"nebius.ai/slurm-operator/internal/e2e/acceptance"
)

func RunAcceptance(ctx context.Context, _ Config) error {
	runner := acceptance.NewRunner()

	if err := runner.Run(ctx); err != nil {
		return fmt.Errorf("run acceptance suite: %w", err)
	}

	return nil
}
