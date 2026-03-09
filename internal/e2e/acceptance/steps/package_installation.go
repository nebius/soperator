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
	exec framework.Executor
}

func NewPackageInstallation(_ *framework.SharedState, exec framework.Executor) PackageInstallation {
	return PackageInstallation{exec: exec}
}

func (s PackageInstallation) Register(sc *godog.ScenarioContext) {
	sc.Step(`^packages can be installed on the worker without breaking the NVIDIA driver$`, s.packagesCanBeInstalledOnTheWorkerWithoutBreakingTheNVIDIADriver)
}

func (s PackageInstallation) packagesCanBeInstalledOnTheWorkerWithoutBreakingTheNVIDIADriver(ctx context.Context) error {
	worker, err := s.exec.AnyWorker()
	if err != nil {
		return err
	}

	steps := []string{
		fmt.Sprintf("ssh %s 'nvidia-smi >/dev/null'", framework.ShellQuote(worker.Name)),
		fmt.Sprintf("ssh %s 'DEBIAN_FRONTEND=noninteractive apt-get update'", framework.ShellQuote(worker.Name)),
		fmt.Sprintf("ssh %s 'DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends nvitop'", framework.ShellQuote(worker.Name)),
		fmt.Sprintf("ssh %s 'nvidia-smi >/dev/null'", framework.ShellQuote(worker.Name)),
		fmt.Sprintf("ssh %s 'nvitop --help >/dev/null'", framework.ShellQuote(worker.Name)),
	}

	for _, step := range steps {
		if _, err := s.exec.ExecJailWithRetry(ctx, step, 5, 10*time.Second); err != nil {
			s.logInstallFailureDiagnostics(ctx, worker.Name)
			return fmt.Errorf("package installation step failed (%s): %w", step, err)
		}
	}

	return nil
}

func (s PackageInstallation) logInstallFailureDiagnostics(ctx context.Context, workerName string) {
	commands := []string{
		fmt.Sprintf("ssh %s 'dpkg --audit || true'", framework.ShellQuote(workerName)),
		fmt.Sprintf("ssh %s 'apt-cache policy nvitop || true'", framework.ShellQuote(workerName)),
		fmt.Sprintf("ssh %s 'tail -n 60 /var/log/dpkg.log || true'", framework.ShellQuote(workerName)),
		fmt.Sprintf("ssh %s 'tail -n 60 /var/log/apt/term.log || true'", framework.ShellQuote(workerName)),
	}

	for _, command := range commands {
		output, err := s.exec.ExecJailWithRetry(ctx, command, 2, 10*time.Second)
		if err != nil {
			s.exec.Logf("package installation debug command failed (%s): %v", command, err)
			continue
		}
		s.exec.Logf("package installation debug output (%s):\n%s", command, strings.TrimSpace(output))
	}
}
