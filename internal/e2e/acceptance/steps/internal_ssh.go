package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

type InternalSSH struct {
	state    *framework.SharedState
	scenario *framework.ScenarioState
	exec     framework.Executor
}

func NewInternalSSH(state *framework.SharedState, scenario *framework.ScenarioState, exec framework.Executor) InternalSSH {
	return InternalSSH{state: state, scenario: scenario, exec: exec}
}

func (s InternalSSH) Register(sc *godog.ScenarioContext) {
	sc.Step(`^a regular user account exists on the login node$`, s.aRegularUserAccountExistsOnTheLoginNode)
	sc.Step(`^the user SSHs from the login node to a worker$`, s.theUserSSHsFromTheLoginNodeToAWorker)
	sc.Step(`^the connection succeeds without extra SSH options$`, s.theConnectionSucceedsWithoutExtraSSHOptions)
}

func (s InternalSSH) aRegularUserAccountExistsOnTheLoginNode(ctx context.Context) error {
	worker, err := s.exec.AnyWorker()
	if err != nil {
		return err
	}
	s.scenario.TargetWorker = worker

	userName := s.state.InternalSSH.UserName
	if userName == "" {
		userName = "bob"
	}
	cmd := fmt.Sprintf("id %s >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh %s",
		framework.ShellQuote(userName), framework.ShellQuote(userName))
	if _, err := s.exec.ExecJailWithRetry(ctx, cmd, 5, 10*time.Second); err != nil {
		return fmt.Errorf("create user %s: %w", userName, err)
	}

	return nil
}

func (s InternalSSH) theUserSSHsFromTheLoginNodeToAWorker(ctx context.Context) error {
	userName := s.state.InternalSSH.UserName
	if userName == "" {
		userName = "bob"
	}
	cmd := fmt.Sprintf("su - %s -c 'timeout 30 ssh %s hostname </dev/null'",
		framework.ShellQuote(userName), framework.ShellQuote(s.scenario.TargetWorker.Name))
	out, err := s.exec.ExecJailWithRetry(ctx, cmd, 5, 10*time.Second)
	if err != nil {
		return fmt.Errorf("ssh from login to worker as %s: %w", userName, err)
	}
	s.scenario.SSHOutput = out
	return nil
}

func (s InternalSSH) theConnectionSucceedsWithoutExtraSSHOptions() error {
	if !strings.Contains(s.scenario.SSHOutput, s.scenario.TargetWorker.Name) {
		return fmt.Errorf("unexpected ssh output %q, expected hostname %q",
			strings.TrimSpace(s.scenario.SSHOutput), s.scenario.TargetWorker.Name)
	}
	return nil
}
