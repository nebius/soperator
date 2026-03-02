package e2e

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-exec/tfexec"
)

func Apply(ctx context.Context, cfg Config) error {
	tf, varFilePath, cleanup, err := Init(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := tf.Apply(ctx, tfexec.VarFile(varFilePath)); err != nil {
		return fmt.Errorf("terraform apply: %w", err)
	}
	return nil
}
