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

	suite.Given(ctx, "a worker is selected for the SSH check", func(_ SpecContext) {
		worker, err := suite.AnyWorker()
		Expect(err).NotTo(HaveOccurred())
		state.targetWorker = worker
	})

	suite.And(ctx, "the regular user exists on the login node", func(ctx SpecContext) {
		createUserCmd := fmt.Sprintf(
			"id %s >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh %s",
			framework.ShellQuote(sshUserName),
			framework.ShellQuote(sshUserName),
		)
		_, err := suite.ExecJailWithRetry(ctx, createUserCmd, 5, 10*time.Second)
		Expect(err).NotTo(HaveOccurred())
	})

	suite.When(ctx, "the regular user SSHes from the login node to the selected worker", func(ctx SpecContext) {
		sshCmd := fmt.Sprintf(
			"su - %s -c 'timeout 30 ssh %s hostname </dev/null'",
			framework.ShellQuote(sshUserName),
			framework.ShellQuote(state.targetWorker.Name),
		)
		out, err := suite.ExecJailWithRetry(ctx, sshCmd, 5, 10*time.Second)
		Expect(err).NotTo(HaveOccurred())
		state.sshOutput = out
	})

	suite.Then(ctx, "the SSH target reports the selected worker hostname", func(_ SpecContext) {
		Expect(strings.TrimSpace(state.sshOutput)).To(ContainSubstring(state.targetWorker.Name))
	})
}
