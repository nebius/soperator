package testenv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
)

// Run executes the provided command within this context
func Run(cmd *exec.Cmd) (string, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "chdir dir: %s\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %s\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s failed with error: (%v) %s", command, err, string(output))
	}

	return string(output), nil
}

// InstallKind installs kind CLI to ./bin/
func InstallKind(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "make", "install-kind")
	_, err := Run(cmd)
	return err
}

// InstallYq installs yq CLI to ./bin/
func InstallYq(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "make", "yq")
	_, err := Run(cmd)
	return err
}

// CreateKindCluster creates a kind cluster with specified nodes
func CreateKindCluster(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "make", "kind-create")
	_, err := Run(cmd)
	return err
}

// DeleteKindCluster deletes the kind cluster
func DeleteKindCluster(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "make", "kind-delete")
	_, err := Run(cmd)
	return err
}

// RestartKindCluster restarts the kind cluster (delete + create)
func RestartKindCluster(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "make", "kind-restart")
	_, err := Run(cmd)
	return err
}

// GetKindClusterStatus checks kind cluster status and deployments
func GetKindClusterStatus(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "make", "kind-status")
	return Run(cmd)
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.Split(output, "\n")
	for _, element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	wd = strings.Replace(wd, "/test/integration", "", -1)
	wd = strings.Replace(wd, "/test/e2e", "", -1)
	return wd, nil
}
