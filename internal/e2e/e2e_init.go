package e2e

import (
	"context"
)

func RunInit(ctx context.Context, cfg Config) error {
	tf, varFilePath, cleanup, err := Init(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	// Recover from the SCHED-1401 leak pattern: a previous run's terraform
	// destroy failed with BucketNotEmpty and left the backups bucket (plus
	// objects) behind. On that next run cleanup_bucket is already out of
	// state so tf destroy can't re-empty the bucket; empty it here first.
	bestEffortEmptyBackupsBucket(ctx)

	return destroyWithK8sRecovery(ctx, tf, varFilePath, cfg.Profile.NebiusProjectID)
}
