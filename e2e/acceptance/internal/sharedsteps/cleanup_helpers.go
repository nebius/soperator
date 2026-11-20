package sharedsteps

import (
	"context"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

// runCleanupPhase runs one best-effort cleanup step under its own deadline,
// even after the scenario context is cancelled, and logs its failure instead of returning it.
func runCleanupPhase(ctx context.Context, logger framework.Logger, name string, timeout time.Duration, phase func(ctx context.Context) error) {
	phaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	if err := phase(phaseCtx); err != nil {
		logger.Logf("Cleanup: %s: %v", name, err)
	}
}
