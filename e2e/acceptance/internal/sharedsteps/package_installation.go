package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type PackageInstallation struct {
	runtime       framework.Runtime
	selector      *framework.WorkerSelector
	packageWorker framework.WorkerInfo
}

func NewPackageInstallation(runtime framework.Runtime, selector *framework.WorkerSelector) *PackageInstallation {
	return &PackageInstallation{runtime: runtime, selector: selector}
}

func (s *PackageInstallation) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the NVIDIA driver is working on a worker node$`, s.theNVIDIADriverIsWorkingOnAWorkerNode)
	sc.Step(`^jq is installed on the worker node$`, s.jqIsInstalledOnTheWorkerNode)
	sc.Step(`^the NVIDIA driver is still working on the worker node$`, s.theNVIDIADriverIsStillWorkingOnTheWorkerNode)
	sc.Step(`^jq is available on the worker node$`, s.jqIsAvailableOnTheWorkerNode)
}

func (s *PackageInstallation) CleanupAndReset(ctx context.Context) {
	s.packageWorker = framework.WorkerInfo{}
}

func (s *PackageInstallation) theNVIDIADriverIsWorkingOnAWorkerNode(ctx context.Context) error {
	workers, err := s.selector.PickGPUWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.packageWorker = workers[0]

	if _, err := s.runtime.Worker(s.packageWorker).RunWithDefaultRetry(ctx, "nvidia-smi >/dev/null"); err != nil {
		s.logInstallFailureDiagnostics(ctx, s.packageWorker)
		return fmt.Errorf("verify nvidia-smi before install: %w", err)
	}
	return nil
}

func (s *PackageInstallation) jqIsInstalledOnTheWorkerNode(ctx context.Context) error {
	// TODO(SCHED-1498): switch this test back to installing nvitop.
	// nvitop currently pulls NVIDIA user-space packages, and dpkg fails in our jail/chroot layout
	// with "Invalid cross-device link" when creating backup hardlinks during package replacement.
	updateCmd := "DEBIAN_FRONTEND=noninteractive apt-get update"
	if _, err := s.runtime.Worker(s.packageWorker).Run(ctx, updateCmd); err != nil {
		s.logInstallFailureDiagnostics(ctx, s.packageWorker)
		return fmt.Errorf("apt-get update: %w", err)
	}

	installCmd := "DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends jq"
	if _, err := s.runtime.Worker(s.packageWorker).Run(ctx, installCmd); err != nil {
		s.logInstallFailureDiagnostics(ctx, s.packageWorker)
		return fmt.Errorf("apt-get install jq: %w", err)
	}
	return nil
}

func (s *PackageInstallation) theNVIDIADriverIsStillWorkingOnTheWorkerNode(ctx context.Context) error {
	if _, err := s.runtime.Worker(s.packageWorker).RunWithDefaultRetry(ctx, "nvidia-smi >/dev/null"); err != nil {
		s.logInstallFailureDiagnostics(ctx, s.packageWorker)
		return fmt.Errorf("verify nvidia-smi after install: %w", err)
	}
	return nil
}

func (s *PackageInstallation) jqIsAvailableOnTheWorkerNode(ctx context.Context) error {
	if _, err := s.runtime.Worker(s.packageWorker).RunWithDefaultRetry(ctx, "jq --version >/dev/null"); err != nil {
		s.logInstallFailureDiagnostics(ctx, s.packageWorker)
		return fmt.Errorf("verify jq after install: %w", err)
	}
	return nil
}

func (s *PackageInstallation) logInstallFailureDiagnostics(ctx context.Context, worker framework.WorkerInfo) {
	commands := []string{
		"dpkg --audit || true",
		"apt-cache policy jq || true",
		"tail -n 60 /var/log/dpkg.log || true",
		"tail -n 60 /var/log/apt/term.log || true",
	}

	for _, command := range commands {
		output, err := s.runtime.Worker(worker).RunWithRetry(ctx, command, 2, 10*time.Second)
		if err != nil {
			s.runtime.Logf("package installation debug command failed on %s (%s): %v", worker.Name, command, err)
			continue
		}
		s.runtime.Logf("package installation debug output on %s (%s):\n%s", worker.Name, command, strings.TrimSpace(output))
	}
}
