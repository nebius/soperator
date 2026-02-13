package e2e

import (
	"context"
	"fmt"
	"log"

	"github.com/hashicorp/terraform-exec/tfexec"
)

func Apply(ctx context.Context, cfg Config) error {
	tf, varFilePath, cleanup, err := Init(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := tf.Destroy(ctx, tfexec.VarFile(varFilePath)); err != nil {
		log.Printf("Pre-cleanup destroy failed: %v", err)
		logState(ctx, tf)
		return fmt.Errorf("pre-cleanup destroy failed, state may contain stuck resources: %w", err)
	}

	if err := tf.Apply(ctx, tfexec.VarFile(varFilePath)); err != nil {
		return fmt.Errorf("terraform apply: %w", err)
	}
	return nil
}
