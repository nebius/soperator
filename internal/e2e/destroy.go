package e2e

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"nebius.ai/slurm-operator/internal/e2e/tfrunner"
)

const k8sClusterName = "soperator-e2e-test"

func Destroy(cfg Config) error {
	runner, cleanup, err := Init(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	return destroyWithK8sRecovery(runner)
}

func destroyWithK8sRecovery(runner *tfrunner.Runner) error {
	err := runner.Destroy()
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "Kubernetes cluster unreachable") {
		return err
	}
	if !isMK8SClusterGone() {
		return err
	}
	log.Print("K8s cluster is confirmed gone, removing helm releases from state and retrying destroy")
	removeHelmReleasesFromState(runner)
	logState(runner)
	if retryErr := runner.Destroy(); retryErr != nil {
		return fmt.Errorf("destroy after helm release state cleanup: %w", retryErr)
	}
	log.Printf("Destroy recovered: K8s cluster %s was already gone, removed helm releases from state to unblock cleanup", k8sClusterName)
	return nil
}

func removeHelmReleasesFromState(runner *tfrunner.Runner) {
	out, err := runner.Run("state", "list")
	if err != nil {
		log.Printf("terraform state list failed during helm release removal: %v", err)
		return
	}
	for _, resource := range strings.Split(out, "\n") {
		resource = strings.TrimSpace(resource)
		if resource == "" || !strings.Contains(resource, "helm_release") {
			continue
		}
		log.Printf("Removing %s from terraform state", resource)
		if _, rmErr := runner.Run("state", "rm", resource); rmErr != nil {
			log.Printf("terraform state rm %s failed: %v", resource, rmErr)
		}
	}
}

func isMK8SClusterGone() bool {
	projectID := os.Getenv("NEBIUS_PROJECT_ID")
	if projectID == "" {
		log.Print("NEBIUS_PROJECT_ID not set, cannot verify cluster existence")
		return false
	}

	out, err := exec.CommandContext(context.Background(),
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
