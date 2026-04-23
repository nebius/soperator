package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const sshUserName = "bob"

type InternalSSH struct {
	exec         framework.Exec
	slurm        *framework.SlurmClient
	targetWorker string
	sshOutput    string
}

func NewInternalSSH(exec framework.Exec, slurm *framework.SlurmClient) *InternalSSH {
	return &InternalSSH{exec: exec, slurm: slurm}
}

func (s *InternalSSH) Register(sc *godog.ScenarioContext) {
	sc.Step(`^a regular user account exists on the login node$`, s.aRegularUserAccountExistsOnTheLoginNode)
	sc.Step(`^the user SSHs from the login node to a worker$`, s.theUserSSHsFromTheLoginNodeToAWorker)
	sc.Step(`^the connection succeeds without extra SSH options$`, s.theConnectionSucceedsWithoutExtraSSHOptions)
}

func (s *InternalSSH) aRegularUserAccountExistsOnTheLoginNode(ctx context.Context) error {
	workers, err := s.slurm.AnyWorkers(1)
	if err != nil {
		return err
	}
	s.targetWorker = workers[0]

	cmd := fmt.Sprintf("id %s >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh %s",
		framework.ShellQuote(sshUserName), framework.ShellQuote(sshUserName))
	if _, err := s.exec.Jail().Run(ctx, cmd); err != nil {
		return fmt.Errorf("create user %s: %w", sshUserName, err)
	}

	return nil
}

func (s *InternalSSH) theUserSSHsFromTheLoginNodeToAWorker(ctx context.Context) error {
	worker := framework.ShellQuote(s.targetWorker)
	// Remove the worker key before each SSH attempt so retries don't depend on
	// persisted known_hosts state from previous attempts.
	cmd := fmt.Sprintf("su - %s -c %s",
		framework.ShellQuote(sshUserName),
		framework.ShellQuote(fmt.Sprintf(
			"mkdir -p ~/.ssh && touch ~/.ssh/known_hosts && (ssh-keygen -R %s -f ~/.ssh/known_hosts >/dev/null 2>&1 || true) && timeout 30 ssh %s hostname </dev/null",
			worker,
			worker,
		)),
	)
	out, err := s.exec.Jail().RunWithDefaultRetry(ctx, cmd)
	if err != nil {
		return fmt.Errorf("ssh from login to worker as %s: %w", sshUserName, err)
	}
	s.sshOutput = out
	return nil
}

func (s *InternalSSH) theConnectionSucceedsWithoutExtraSSHOptions() error {
	if !strings.Contains(s.sshOutput, s.targetWorker) {
		return fmt.Errorf("unexpected ssh output %q, expected hostname %q",
			strings.TrimSpace(s.sshOutput), s.targetWorker)
	}
	return nil
}
