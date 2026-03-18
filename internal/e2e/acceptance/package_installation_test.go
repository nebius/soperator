//go:build acceptance

package acceptance

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

type packageInstallationScenario struct {
	targetWorker framework.WorkerRef
}

func packageInstallationTest(ctx SpecContext) {
	state := packageInstallationScenario{}

	suite.Given(ctx, "a worker is selected for package installation", func(_ SpecContext) {
		worker, err := suite.AnyWorker()
		Expect(err).NotTo(HaveOccurred())
		state.targetWorker = worker
	})

	suite.Then(ctx, "nvidia-smi works before jq is installed", func(ctx SpecContext) {
		nvidiaCmd := fmt.Sprintf("ssh %s 'nvidia-smi >/dev/null'", framework.ShellQuote(state.targetWorker.Name))
		_, err := suite.ExecJailWithRetry(ctx, nvidiaCmd, 5, 10*time.Second)
		if err != nil {
			logInstallFailureDiagnostics(ctx, state.targetWorker.Name)
		}
		Expect(err).NotTo(HaveOccurred())
	})

	suite.When(ctx, "jq is installed on the worker", func(ctx SpecContext) {
		updateCmd := fmt.Sprintf("ssh %s 'DEBIAN_FRONTEND=noninteractive apt-get update'", framework.ShellQuote(state.targetWorker.Name))
		_, err := suite.ExecJailWithRetry(ctx, updateCmd, 5, 10*time.Second)
		if err != nil {
			logInstallFailureDiagnostics(ctx, state.targetWorker.Name)
		}
		Expect(err).NotTo(HaveOccurred())

		installCmd := fmt.Sprintf("ssh %s 'DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends jq'", framework.ShellQuote(state.targetWorker.Name))
		_, err = suite.ExecJailWithRetry(ctx, installCmd, 5, 10*time.Second)
		if err != nil {
			logInstallFailureDiagnostics(ctx, state.targetWorker.Name)
		}
		Expect(err).NotTo(HaveOccurred())
	})

	suite.Then(ctx, "nvidia-smi still works after jq is installed", func(ctx SpecContext) {
		nvidiaCmd := fmt.Sprintf("ssh %s 'nvidia-smi >/dev/null'", framework.ShellQuote(state.targetWorker.Name))
		_, err := suite.ExecJailWithRetry(ctx, nvidiaCmd, 5, 10*time.Second)
		if err != nil {
			logInstallFailureDiagnostics(ctx, state.targetWorker.Name)
		}
		Expect(err).NotTo(HaveOccurred())
	})

	suite.And(ctx, "jq is available on the worker", func(ctx SpecContext) {
		jqCmd := fmt.Sprintf("ssh %s 'jq --version >/dev/null'", framework.ShellQuote(state.targetWorker.Name))
		_, err := suite.ExecJailWithRetry(ctx, jqCmd, 5, 10*time.Second)
		if err != nil {
			logInstallFailureDiagnostics(ctx, state.targetWorker.Name)
		}
		Expect(err).NotTo(HaveOccurred())
	})
}

func logInstallFailureDiagnostics(ctx SpecContext, workerName string) {
	commands := []string{
		fmt.Sprintf("ssh %s 'dpkg --audit || true'", framework.ShellQuote(workerName)),
		fmt.Sprintf("ssh %s 'apt-cache policy jq || true'", framework.ShellQuote(workerName)),
		fmt.Sprintf("ssh %s 'tail -n 60 /var/log/dpkg.log || true'", framework.ShellQuote(workerName)),
		fmt.Sprintf("ssh %s 'tail -n 60 /var/log/apt/term.log || true'", framework.ShellQuote(workerName)),
	}

	for _, command := range commands {
		output, err := suite.ExecJailWithRetry(ctx, command, 2, 10*time.Second)
		if err != nil {
			suite.Logf("package installation debug command failed (%s): %v", command, err)
			continue
		}
		suite.Logf("package installation debug output (%s):\n%s", command, strings.TrimSpace(output))
	}
}
