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

	suite.Step(ctx, "selecting a worker for the SSH check", "Selected a worker node as the SSH target.", func() string {
		if state.targetWorker.Name == "" {
			return "Worker target has not been selected yet."
		}
		return fmt.Sprintf("Selected worker: %s", state.targetWorker.Name)
	}, func(_ SpecContext) {
		worker, err := suite.AnyWorker()
		Expect(err).NotTo(HaveOccurred())
		state.targetWorker = worker
	})

	suite.Step(ctx, "ensuring the regular user exists on the login node", "Verified that a regular user account exists on the login node.", func() string {
		return fmt.Sprintf("Username: %s", sshUserName)
	}, func(ctx SpecContext) {
		createUserCmd := fmt.Sprintf(
			"id %s >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh %s",
			framework.ShellQuote(sshUserName),
			framework.ShellQuote(sshUserName),
		)
		_, err := suite.ExecJailWithRetry(ctx, createUserCmd, 5, 10*time.Second)
		Expect(err).NotTo(HaveOccurred())
	})

	suite.Step(ctx, "SSHing from the login node to the selected worker", "Attempted an SSH connection from the login node to the selected worker.", func() string {
		return fmt.Sprintf("Worker: %s\nSSH output:\n%s", state.targetWorker.Name, strings.TrimSpace(state.sshOutput))
	}, func(ctx SpecContext) {
		sshCmd := fmt.Sprintf(
			"su - %s -c 'timeout 30 ssh %s hostname </dev/null'",
			framework.ShellQuote(sshUserName),
			framework.ShellQuote(state.targetWorker.Name),
		)
		out, err := suite.ExecJailWithRetry(ctx, sshCmd, 5, 10*time.Second)
		Expect(err).NotTo(HaveOccurred())
		state.sshOutput = out
	})

	suite.Step(ctx, "checking that the SSH target reports the worker hostname", "Confirmed that the SSH session reached the intended worker.", func() string {
		return fmt.Sprintf("Expected hostname: %s\nObserved output:\n%s", state.targetWorker.Name, strings.TrimSpace(state.sshOutput))
	}, func(_ SpecContext) {
		Expect(strings.TrimSpace(state.sshOutput)).To(ContainSubstring(state.targetWorker.Name))
	})
}
