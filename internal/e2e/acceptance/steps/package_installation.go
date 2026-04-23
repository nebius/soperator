package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

type PackageInstallation struct {
	exec          framework.Exec
	slurm         *framework.SlurmClient
	packageWorker string
}

func NewPackageInstallation(exec framework.Exec, slurm *framework.SlurmClient) *PackageInstallation {
	return &PackageInstallation{exec: exec, slurm: slurm}
}

func (s *PackageInstallation) Register(sc *godog.ScenarioContext) {
	sc.Step(`^the NVIDIA driver is working on a worker node$`, s.theNVIDIADriverIsWorkingOnAWorkerNode)
	sc.Step(`^jq is installed on the worker node$`, s.jqIsInstalledOnTheWorkerNode)
	sc.Step(`^the NVIDIA driver is still working on the worker node$`, s.theNVIDIADriverIsStillWorkingOnTheWorkerNode)
	sc.Step(`^jq is available on the worker node$`, s.jqIsAvailableOnTheWorkerNode)
}

func (s *PackageInstallation) theNVIDIADriverIsWorkingOnAWorkerNode(ctx context.Context) error {
	workers, err := s.slurm.AnyGPUWorkers(1)
	if err != nil {
		return err
	}
	s.packageWorker = workers[0]

	cmd := fmt.Sprintf("ssh %s 'nvidia-smi >/dev/null'", framework.ShellQuote(s.packageWorker))
	if _, err := s.exec.Jail().RunWithDefaultRetry(ctx, cmd); err != nil {
		s.logInstallFailureDiagnostics(ctx, s.packageWorker)
		return fmt.Errorf("verify nvidia-smi before install: %w", err)
	}
	return nil
}

func (s *PackageInstallation) jqIsInstalledOnTheWorkerNode(ctx context.Context) error {
	// TODO(SCHED-1498): switch this test back to installing nvitop.
	// nvitop currently pulls NVIDIA user-space packages, and dpkg fails in our jail/chroot layout
	// with "Invalid cross-device link" when creating backup hardlinks during package replacement.
	workerName := s.packageWorker
	updateCmd := fmt.Sprintf("ssh %s 'DEBIAN_FRONTEND=noninteractive apt-get update'", framework.ShellQuote(workerName))
	if _, err := s.exec.Jail().Run(ctx, updateCmd); err != nil {
		s.logInstallFailureDiagnostics(ctx, workerName)
		return fmt.Errorf("apt-get update: %w", err)
	}

	installCmd := fmt.Sprintf("ssh %s 'DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends jq'", framework.ShellQuote(workerName))
	if _, err := s.exec.Jail().Run(ctx, installCmd); err != nil {
		s.logInstallFailureDiagnostics(ctx, workerName)
		return fmt.Errorf("apt-get install jq: %w", err)
	}
	return nil
}

func (s *PackageInstallation) theNVIDIADriverIsStillWorkingOnTheWorkerNode(ctx context.Context) error {
	workerName := s.packageWorker
	cmd := fmt.Sprintf("ssh %s 'nvidia-smi >/dev/null'", framework.ShellQuote(workerName))
	if _, err := s.exec.Jail().RunWithDefaultRetry(ctx, cmd); err != nil {
		s.logInstallFailureDiagnostics(ctx, workerName)
		return fmt.Errorf("verify nvidia-smi after install: %w", err)
	}
	return nil
}

func (s *PackageInstallation) jqIsAvailableOnTheWorkerNode(ctx context.Context) error {
	workerName := s.packageWorker
	cmd := fmt.Sprintf("ssh %s 'jq --version >/dev/null'", framework.ShellQuote(workerName))
	if _, err := s.exec.Jail().RunWithDefaultRetry(ctx, cmd); err != nil {
		s.logInstallFailureDiagnostics(ctx, workerName)
		return fmt.Errorf("verify jq after install: %w", err)
	}
	return nil
}

func (s *PackageInstallation) logInstallFailureDiagnostics(ctx context.Context, workerName string) {
	commands := []string{
		fmt.Sprintf("ssh %s 'dpkg --audit || true'", framework.ShellQuote(workerName)),
		fmt.Sprintf("ssh %s 'apt-cache policy jq || true'", framework.ShellQuote(workerName)),
		fmt.Sprintf("ssh %s 'tail -n 60 /var/log/dpkg.log || true'", framework.ShellQuote(workerName)),
		fmt.Sprintf("ssh %s 'tail -n 60 /var/log/apt/term.log || true'", framework.ShellQuote(workerName)),
	}

	for _, command := range commands {
		output, err := s.exec.Jail().RunWithRetry(ctx, command, 2, 10*time.Second)
		if err != nil {
			s.exec.Logf("package installation debug command failed (%s): %v", command, err)
			continue
		}
		s.exec.Logf("package installation debug output (%s):\n%s", command, strings.TrimSpace(output))
	}
}
