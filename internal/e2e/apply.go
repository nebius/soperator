package e2e

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-exec/tfexec"
)

const (
	terraformApplyTimeout = 90 * time.Minute
	terraformStopTimeout  = time.Minute
)

func Apply(ctx context.Context, cfg Config) error {
	ctx, cancel := context.WithTimeout(ctx, terraformApplyTimeout)
	defer cancel()

	tf, varFilePath, cleanup, err := Init(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := tf.SetWaitDelay(terraformStopTimeout); err != nil {
		return fmt.Errorf("set Terraform graceful shutdown timeout: %w", err)
	}

	if err := tf.Apply(ctx, tfexec.VarFile(varFilePath)); err != nil {
		return fmt.Errorf("terraform apply: %w", err)
	}
	return nil
}
