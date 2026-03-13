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

const sshUserName = "bob"

type sshScenario struct {
	targetWorker framework.WorkerRef
	sshOutput    string
}

func internalSSHTest(ctx SpecContext) {
	state := sshScenario{}

	suite.Detail("username", sshUserName)

	suite.Step(ctx, "selecting a worker for the SSH check", func(_ SpecContext, step *framework.StepRecorder) {
		worker, err := suite.AnyWorker()
		Expect(err).NotTo(HaveOccurred())
		state.targetWorker = worker
		suite.Detail("worker", state.targetWorker.Name)
	})

	suite.Step(ctx, "ensuring the regular user exists on the login node", func(ctx SpecContext, step *framework.StepRecorder) {
		createUserCmd := fmt.Sprintf(
			"id %s >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh %s",
			framework.ShellQuote(sshUserName),
			framework.ShellQuote(sshUserName),
		)
		_, err := suite.ExecJailWithRetry(ctx, createUserCmd, 5, 10*time.Second)
		Expect(err).NotTo(HaveOccurred())
	})

	suite.Step(ctx, "SSHing from the login node to the selected worker", func(ctx SpecContext, step *framework.StepRecorder) {
		sshCmd := fmt.Sprintf(
			"su - %s -c 'timeout 30 ssh %s hostname </dev/null'",
			framework.ShellQuote(sshUserName),
			framework.ShellQuote(state.targetWorker.Name),
		)
		out, err := suite.ExecJailWithRetry(ctx, sshCmd, 5, 10*time.Second)
		Expect(err).NotTo(HaveOccurred())
		state.sshOutput = out
		step.Detail("observed_hostname", strings.TrimSpace(state.sshOutput))
	})

	suite.Step(ctx, "checking that the SSH target reports the worker hostname", func(_ SpecContext, step *framework.StepRecorder) {
		step.Detail("observed_hostname", strings.TrimSpace(state.sshOutput))
		Expect(strings.TrimSpace(state.sshOutput)).To(ContainSubstring(state.targetWorker.Name))
	})
}
