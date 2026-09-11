package e2e

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// tfDestroyLogPathEnvVar, when set, enables terraform DEBUG logging for the
// destroy flow and writes it to the given file. terraform-exec strips TF_LOG
// from the child process env unless SetLogPath has been called on the handle,
// so this opt-in path is the only way to get terraform logs out of bin/e2e
// destroy. The file is typically uploaded as a CI artifact afterwards.
//
// Only Terraform core logging is enabled. Provider DEBUG logging is excluded
// because providers may log raw credentials such as short-lived IAM tokens.
// Core logs still contain operational identifiers and must be treated as
// internal diagnostics.
const tfDestroyLogPathEnvVar = "TF_DESTROY_LOG_PATH"

func Destroy(ctx context.Context, cfg Config) error {
	tf, varFilePath, cleanup, err := Init(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	enableTFDestroyLogging(tf)

	if err := destroyWithK8sRecovery(ctx, tf, varFilePath, cfg.Profile.NebiusProjectID); err != nil {
		return err
	}

	// State is now empty; drop the saved bundle so the next fresh run has nothing to clean up.
	deleteBundle(ctx)
	return nil
}

func enableTFDestroyLogging(tf *tfexec.Terraform) {
	path := os.Getenv(tfDestroyLogPathEnvVar)
	if path == "" {
		return
	}
	// SetLogCore must precede SetLogPath. Otherwise terraform-exec enables global
	// TRACE logging by default, which also enables unsafe provider logging.
	if err := tf.SetLogCore("DEBUG"); err != nil {
		log.Printf("SetLogCore(DEBUG) failed, continuing without terraform debug logging: %v", err)
		return
	}
	if err := tf.SetLogPath(path); err != nil {
		log.Printf("SetLogPath(%q) failed, continuing without terraform debug logging: %v", path, err)
		return
	}
	log.Printf("Terraform core debug logging enabled, writing to %s", path)
}

func destroyWithK8sRecovery(ctx context.Context, tf *tfexec.Terraform, varFilePath, nebiusProjectID string) error {
	err := tf.Destroy(ctx, tfexec.VarFile(varFilePath))
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "Kubernetes cluster unreachable") {
		return err
	}
	if !isMK8SClusterGone(ctx, nebiusProjectID) {
		return err
	}
	log.Print("K8s cluster is confirmed gone, removing helm releases from state and retrying destroy")
	removeHelmReleasesFromState(ctx, tf)
	logState(ctx, tf)
	if retryErr := tf.Destroy(ctx, tfexec.VarFile(varFilePath)); retryErr != nil {
		return fmt.Errorf("destroy after helm release state cleanup: %w", retryErr)
	}
	log.Printf("Destroy recovered: K8s cluster %s was already gone, removed helm releases from state to unblock cleanup", k8sClusterName)
	return nil
}

func removeHelmReleasesFromState(ctx context.Context, tf *tfexec.Terraform) {
	state, err := tf.Show(ctx)
	if err != nil {
		log.Printf("terraform show failed during helm release removal: %v", err)
		return
	}
	for _, addr := range collectResourceAddresses(state) {
		if !strings.Contains(addr, "helm_release") {
			continue
		}
		log.Printf("Removing %s from terraform state", addr)
		if err := tf.StateRm(ctx, addr); err != nil {
			log.Printf("terraform state rm %s failed: %v", addr, err)
		}
	}
}

func isMK8SClusterGone(ctx context.Context, nebiusProjectID string) bool {
	if nebiusProjectID == "" {
		log.Print("nebius project ID is empty, cannot verify cluster existence")
		return false
	}

	out, err := exec.CommandContext(ctx,
		"nebius", "mk8s", "cluster", "get-by-name",
		"--parent-id", nebiusProjectID,
		"--name", k8sClusterName,
	).CombinedOutput()
	if err != nil {
		log.Printf("mk8s cluster %s not found (get-by-name failed: %v, output: %s)", k8sClusterName, err, string(out))
		return true
	}
	log.Printf("mk8s cluster %s still exists", k8sClusterName)
	return false
}
