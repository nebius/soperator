package e2e

import (
	"context"
	"log"
)

// RunInit prepares the current checkout for apply: it runs terraform init and
// selects the e2e-test workspace, then saves the terraform bundle to S3 so the
// next run can destroy whatever this run creates with matching code. The
// destroy of any leftover state from a previous run is handled earlier, by the
// separate `cleanup-previous` command.
func RunInit(ctx context.Context, cfg Config) error {
	_, _, cleanup, err := Init(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	// Save the bundle before the deferred cleanup removes the override var file,
	// and before apply creates any resource. Best-effort: a failed upload only
	// degrades the next run to a current-checkout cleanup, it must not block this
	// run.
	if err := saveBundle(ctx, cfg); err != nil {
		log.Printf("WARNING: failed to save terraform bundle, next run will fall back to current-checkout cleanup: %v", err)
	}
	return nil
}
