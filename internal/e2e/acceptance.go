package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"nebius.ai/slurm-operator/internal/e2e/acceptance"
)

const defaultAcceptanceReportDir = "e2e-reports/acceptance"

func RunAcceptance(ctx context.Context, cfg Config) error {
	kubectlContext, err := currentKubectlContext(ctx)
	if err != nil {
		return err
	}

	if err := acceptance.Run(ctx, acceptanceArgsForConfig(cfg, kubectlContext)); err != nil {
		return fmt.Errorf("run acceptance suite: %w", err)
	}
	return nil
}

func acceptanceArgsForConfig(cfg Config, kubectlContext string) []string {
	return []string{
		"--kubectl-context", kubectlContext,
		"--slurm-cluster-name", cfg.SlurmClusterName,
		fmt.Sprintf("--run-unstable=%t", cfg.RunUnstableTests),
		"--report-dir", defaultAcceptanceReportDir,
	}
}

func currentKubectlContext(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "kubectl", "config", "current-context").Output()
	if err != nil {
		return "", fmt.Errorf("get current kubectl context: %w", err)
	}

	kubectlContext := strings.TrimSpace(string(output))
	if kubectlContext == "" {
		return "", fmt.Errorf("current kubectl context is empty")
	}
	return kubectlContext, nil
}
