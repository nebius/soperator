package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const sshUserName = "bob"

type InternalSSH struct {
	exec         framework.Executor
	targetWorker framework.WorkerRef
	sshOutput    string
}

func NewInternalSSH(exec framework.Executor) *InternalSSH {
	return &InternalSSH{exec: exec}
}

func (s *InternalSSH) Register(sc *godog.ScenarioContext) {
	sc.Step(`^a regular user account exists on the login node$`, s.aRegularUserAccountExistsOnTheLoginNode)
	sc.Step(`^the user SSHs from the login node to a worker$`, s.theUserSSHsFromTheLoginNodeToAWorker)
	sc.Step(`^the connection succeeds without extra SSH options$`, s.theConnectionSucceedsWithoutExtraSSHOptions)
}

func (s *InternalSSH) aRegularUserAccountExistsOnTheLoginNode(ctx context.Context) error {
	worker, err := s.exec.AnyWorker()
	if err != nil {
		return err
	}
	s.targetWorker = worker

	cmd := fmt.Sprintf("id %s >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh %s",
		framework.ShellQuote(sshUserName), framework.ShellQuote(sshUserName))
	if _, err := s.exec.ExecJailWithRetry(ctx, cmd, 5, 10*time.Second); err != nil {
		return fmt.Errorf("create user %s: %w", sshUserName, err)
	}

	return nil
}

func (s *InternalSSH) theUserSSHsFromTheLoginNodeToAWorker(ctx context.Context) error {
	cmd := fmt.Sprintf("su - %s -c 'timeout 30 ssh %s hostname </dev/null'",
		framework.ShellQuote(sshUserName), framework.ShellQuote(s.targetWorker.Name))
	out, err := s.exec.ExecJailWithRetry(ctx, cmd, 5, 10*time.Second)
	if err != nil {
		return fmt.Errorf("ssh from login to worker as %s: %w", sshUserName, err)
	}
	s.sshOutput = out
	return nil
}

func (s *InternalSSH) theConnectionSucceedsWithoutExtraSSHOptions() error {
	if !strings.Contains(s.sshOutput, s.targetWorker.Name) {
		return fmt.Errorf("unexpected ssh output %q, expected hostname %q",
			strings.TrimSpace(s.sshOutput), s.targetWorker.Name)
	}
	return nil
}
