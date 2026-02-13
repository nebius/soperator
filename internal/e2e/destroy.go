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

const k8sClusterName = "soperator-e2e-test"

func Destroy(ctx context.Context, cfg Config) error {
	tf, varFilePath, cleanup, err := Init(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	return destroyWithK8sRecovery(ctx, tf, varFilePath)
}

func destroyWithK8sRecovery(ctx context.Context, tf *tfexec.Terraform, varFilePath string) error {
	err := tf.Destroy(ctx, tfexec.VarFile(varFilePath))
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "Kubernetes cluster unreachable") {
		return err
	}
	if !isMK8SClusterGone(ctx) {
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

func isMK8SClusterGone(ctx context.Context) bool {
	projectID := os.Getenv("NEBIUS_PROJECT_ID")
	if projectID == "" {
		log.Print("NEBIUS_PROJECT_ID not set, cannot verify cluster existence")
		return false
	}

	out, err := exec.CommandContext(ctx,
		"nebius", "mk8s", "cluster", "get-by-name",
		"--parent-id", projectID,
		"--name", k8sClusterName,
	).CombinedOutput()
	if err != nil {
		log.Printf("mk8s cluster %s not found (get-by-name failed: %v, output: %s)", k8sClusterName, err, string(out))
		return true
	}
	log.Printf("mk8s cluster %s still exists", k8sClusterName)
	return false
}
