package e2e

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// RunCleanupPrevious destroys whatever infrastructure a previous e2e run left in
// the shared remote state, before the current run inits and applies.
//
// Stage A destroys with the saved terraform bundle — the exact code that created the leftover state
// — which is immune to provider/schema drift between branches.
// If no bundle exists, or its destroy fails, Stage B falls back to a destroy with the current checkout.
func RunCleanupPrevious(ctx context.Context, cfg Config) error {
	if bundleExists(ctx) {
		if err := cleanupFromBundle(ctx, cfg); err != nil {
			log.Printf("Saved-bundle destroy failed: %v", err)
			log.Printf("Falling back to current checkout")
		} else {
			deleteBundle(ctx)
			return nil
		}
	} else {
		log.Print("No saved terraform bundle, cleaning up leftover state with the current checkout")
	}

	// Stage B: current checkout
	tf, varFilePath, cleanup, err := Init(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := stripHelmThenDestroy(ctx, tf, varFilePath); err != nil {
		return fmt.Errorf("current-checkout cleanup destroy: %w", err)
	}
	deleteBundle(ctx)
	return nil
}

// cleanupFromBundle:
// - downloads and extracts the saved bundle
// - inits terraform against the cached providers/modules (offline)
// - selects the e2e-test workspace, and
// - destroys the leftover resources recorded in the shared remote state.
func cleanupFromBundle(ctx context.Context, cfg Config) error {
	workdir, cleanup, err := downloadAndExtractBundle(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	execPath, err := exec.LookPath("terraform")
	if err != nil {
		return fmt.Errorf("find terraform binary: %w", err)
	}
	tf, err := tfexec.NewTerraform(workdir, execPath)
	if err != nil {
		return fmt.Errorf("create terraform executor for bundle: %w", err)
	}
	tf.SetStdout(os.Stdout)
	tf.SetStderr(os.Stderr)

	// -reconfigure: the bundle carries a .terraform backend cache; reconfigure against the current AWS creds
	// (from the process env) without prompting for state migration.
	if err := initWithRetry(ctx, tf, tfexec.Reconfigure(true)); err != nil {
		return fmt.Errorf("terraform init (bundle): %w", err)
	}
	if err := workspaceSelectOrNew(ctx, tf, "e2e-test"); err != nil {
		return fmt.Errorf("select workspace (bundle): %w", err)
	}
	logState(ctx, tf)

	varFilePath := filepath.Join(workdir, "e2e-override.tfvars.json")
	return stripHelmThenDestroy(ctx, tf, varFilePath)
}

// stripHelmThenDestroy empties the backups bucket, removes all helm_release resources from state,
// then runs terraform destroy.
//
// Removing helm_release first is what makes a leftover-cluster destroy robust:
// helm_release is the only resource type that depends on the cluster host (there are no kubernetes_* resources),
// so once it is out of the plan terraform never needs to configure the helm provider and the destroy cannot wedge on
// "Kubernetes cluster unreachable: no configuration has been provided" (SCHED-1618).
// The helm-installed workloads are torn down when the cluster itself is destroyed.
//
// This is used only for cleaning up previous/leftover runs. The current run's
// final destroy keeps the graceful destroyWithK8sRecovery path.
func stripHelmThenDestroy(ctx context.Context, tf *tfexec.Terraform, varFilePath string) error {
	bestEffortEmptyBackupsBucket(ctx)
	removeHelmReleasesFromState(ctx, tf)
	if err := tf.Destroy(ctx, tfexec.VarFile(varFilePath)); err != nil {
		return fmt.Errorf("terraform destroy: %w", err)
	}
	return nil
}
