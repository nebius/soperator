package testenv

import (
	"context"
	"fmt"
	"os/exec"
)

// InstallFlux installs flux CLI to ./bin/
func InstallFlux(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "make", "install-flux")
	_, err := Run(cmd)
	return err
}

// DeployFlux deploys soperator via Flux CD
// Note: Make sure to call SyncVersion before DeployFlux to sync versions
func DeployFlux(ctx context.Context, unstable bool) error {
	unstableStr := "false"
	if unstable {
		unstableStr = "true"
	}
	cmd := exec.CommandContext(ctx, "make", "deploy-flux", fmt.Sprintf("UNSTABLE=%s", unstableStr))
	_, err := Run(cmd)
	return err
}

// UndeployFlux removes Flux CD configuration
func UndeployFlux(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "make", "undeploy-flux")
	_, err := Run(cmd)
	return err
}
