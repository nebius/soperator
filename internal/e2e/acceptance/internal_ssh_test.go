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

	By("selecting a worker for the SSH check")
	worker, err := suite.AnyWorker()
	Expect(err).NotTo(HaveOccurred())
	state.targetWorker = worker

	By("ensuring the regular user exists on the login node")
	createUserCmd := fmt.Sprintf(
		"id %s >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh %s",
		framework.ShellQuote(sshUserName),
		framework.ShellQuote(sshUserName),
	)
	_, err = suite.ExecJailWithRetry(ctx, createUserCmd, 5, 10*time.Second)
	Expect(err).NotTo(HaveOccurred())

	By("SSHing from the login node to the selected worker")
	sshCmd := fmt.Sprintf(
		"su - %s -c 'timeout 30 ssh %s hostname </dev/null'",
		framework.ShellQuote(sshUserName),
		framework.ShellQuote(state.targetWorker.Name),
	)
	out, err := suite.ExecJailWithRetry(ctx, sshCmd, 5, 10*time.Second)
	Expect(err).NotTo(HaveOccurred())
	state.sshOutput = out

	By("checking that the SSH target reports the worker hostname")
	Expect(strings.TrimSpace(state.sshOutput)).To(ContainSubstring(state.targetWorker.Name))
}
