package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"nebius.ai/slurm-operator/e2e/acceptance"
	"nebius.ai/slurm-operator/e2e/acceptance/framework"
)

const defaultAcceptanceReportDir = "e2e-reports/acceptance"

func RunAcceptance(ctx context.Context, cfg Config) error {
	kubectlContext, err := currentKubectlContext(ctx)
	if err != nil {
		return err
	}

	runner, err := acceptance.NewRunner(acceptanceOptionsForConfig(cfg, kubectlContext))
	if err != nil {
		return fmt.Errorf("configure acceptance suite: %w", err)
	}
	if err := runner.Run(ctx); err != nil {
		return fmt.Errorf("run acceptance suite: %w", err)
	}
	return nil
}

func acceptanceOptionsForConfig(cfg Config, kubectlContext string) acceptance.Options {
	state := &framework.ClusterState{
		SlurmClusterName: cfg.SlurmClusterName,
		WorkersByNodeSet: make(map[string][]framework.WorkerRef),
	}

	return acceptance.Options{
		KubectlContext:            kubectlContext,
		SlurmClusterName:          cfg.SlurmClusterName,
		TargetSoperatorVersion:    cfg.SoperatorVersion,
		ReportDir:                 defaultAcceptanceReportDir,
		Features:                  acceptance.SharedFeatureSource(),
		ExcludeUnstable:           !cfg.RunUnstableTests,
		ExcludeMissingWorkerKinds: true,
		State:                     state,
		StepRegistrars:            []acceptance.StepRegistrar{acceptance.SharedStepRegistrar()},
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
