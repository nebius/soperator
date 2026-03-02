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

	return destroyWithK8sRecovery(ctx, tf, varFilePath, cfg.Profile.NebiusProjectID)
}
