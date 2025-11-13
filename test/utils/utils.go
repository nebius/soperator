package utils

import (
	"bufio"
	"bytes"
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

// IsSlurmClusterCRDsInstalled checks if Slurm Cluster CRDs are installed
func IsSlurmClusterCRDsInstalled(ctx context.Context) bool {
	slurmCRDs := []string{
		"slurmclusters.slurm.nebius.ai",
	}

	cmd := exec.CommandContext(ctx, "kubectl", "get", "crds", "-o", "custom-columns=NAME:.metadata.name")
	output, err := Run(cmd)
	if err != nil {
		return false
	}
	crdList := GetNonEmptyLines(output)
	for _, crd := range slurmCRDs {
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				return true
			}
		}
	}

	return false
}

// IsFluxCDCRDsInstalled checks if Flux CD CRDs are installed
func IsFluxCDCRDsInstalled(ctx context.Context) bool {
	fluxCRDs := []string{
		"helmreleases.helm.toolkit.fluxcd.io",
		"helmrepositories.source.toolkit.fluxcd.io",
		"kustomizations.kustomize.toolkit.fluxcd.io",
		"gitrepositories.source.toolkit.fluxcd.io",
	}

	cmd := exec.CommandContext(ctx, "kubectl", "get", "crds", "-o", "custom-columns=NAME:.metadata.name")
	output, err := Run(cmd)
	if err != nil {
		return false
	}
	crdList := GetNonEmptyLines(output)
	for _, crd := range fluxCRDs {
		found := false
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// IsKruiseCRDsInstalled checks if OpenKruise CRDs are installed
func IsKruiseCRDsInstalled(ctx context.Context) bool {
	kruiseCRDs := []string{
		"clonesets.apps.kruise.io",
		"statefulsets.apps.kruise.io",
		"daemonsets.apps.kruise.io",
	}

	cmd := exec.CommandContext(ctx, "kubectl", "get", "crds", "-o", "custom-columns=NAME:.metadata.name")
	output, err := Run(cmd)
	if err != nil {
		return false
	}
	crdList := GetNonEmptyLines(output)
	for _, crd := range kruiseCRDs {
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				return true
			}
		}
	}

	return false
}

// InstallKind installs kind CLI to ./bin/
func InstallKind(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "make", "install-kind")
	_, err := Run(cmd)
	return err
}

// InstallFlux installs flux CLI to ./bin/
func InstallFlux(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "make", "install-flux")
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

// SyncVersion synchronizes version files with UNSTABLE parameter
func SyncVersion(ctx context.Context, unstable bool) error {
	unstableStr := "false"
	if unstable {
		unstableStr = "true"
	}
	cmd := exec.CommandContext(ctx, "make", "sync-version", fmt.Sprintf("UNSTABLE=%s", unstableStr))
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

// ExecInJail executes a command in the jail environment via login pod
// If no command is provided, opens an interactive shell
func ExecInJail(ctx context.Context, command ...string) (string, error) {
	var cmd *exec.Cmd
	if len(command) == 0 {
		// Interactive shell
		cmd = exec.CommandContext(ctx, "kubectl", "exec", "-it", "login-0", "--", "chroot", "/mnt/jail", "bash")
	} else {
		// Execute specific command
		args := []string{"exec", "login-0", "--", "chroot", "/mnt/jail"}
		args = append(args, command...)
		cmd = exec.CommandContext(ctx, "kubectl", args...)
	}
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
	wd = strings.Replace(wd, "/test/e2e-local", "", -1)
	wd = strings.Replace(wd, "/test/e2e", "", -1)
	return wd, nil
}

// UncommentCode searches for target in the file and remove the comment prefix
// of the target content. The target content may span multiple lines.
func UncommentCode(filename, target, prefix string) error {
	// false positive
	// nolint:gosec
	content, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	strContent := string(content)

	idx := strings.Index(strContent, target)
	if idx < 0 {
		return fmt.Errorf("unable to find the code %s to be uncomment", target)
	}

	out := new(bytes.Buffer)
	_, err = out.Write(content[:idx])
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(bytes.NewBufferString(target))
	if !scanner.Scan() {
		return nil
	}
	for {
		_, err := out.WriteString(strings.TrimPrefix(scanner.Text(), prefix))
		if err != nil {
			return err
		}
		// Avoid writing a newline in case the previous line was the last in target.
		if !scanner.Scan() {
			break
		}
		if _, err := out.WriteString("\n"); err != nil {
			return err
		}
	}

	_, err = out.Write(content[idx+len(target):])
	if err != nil {
		return err
	}
	// false positive
	// nolint:gosec
	return os.WriteFile(filename, out.Bytes(), 0644)
}
