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
	state *framework.SharedState
	exec  framework.Executor
}

func NewInternalSSH(state *framework.SharedState, exec framework.Executor) InternalSSH {
	return InternalSSH{state: state, exec: exec}
}

func (s InternalSSH) Register(sc *godog.ScenarioContext) {
	sc.Step(`^a regular user can SSH from the login node to a worker without extra SSH options$`, s.aRegularUserCanSSHFromTheLoginNodeToAWorkerWithoutExtraSSHOptions)
}

func (s InternalSSH) aRegularUserCanSSHFromTheLoginNodeToAWorkerWithoutExtraSSHOptions(ctx context.Context) error {
	worker, err := s.exec.AnyWorker()
	if err != nil {
		return err
	}

	userName := s.state.InternalSSH.UserName
	if userName == "" {
		userName = "bob"
	}
	if _, err := s.exec.ExecJailWithRetry(ctx, fmt.Sprintf("id %s >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh %s", framework.ShellQuote(userName), framework.ShellQuote(userName)), 5, 10*time.Second); err != nil {
		return fmt.Errorf("create user %s: %w", userName, err)
	}

	cmd := fmt.Sprintf("su - %s -c 'timeout 30 ssh %s hostname </dev/null'", framework.ShellQuote(userName), framework.ShellQuote(worker.Name))
	// This may update the user's known_hosts on first connect; keep that in mind
	// if later scenarios rely on SSH trust state.
	out, err := s.exec.ExecJailWithRetry(ctx, cmd, 5, 10*time.Second)
	if err != nil {
		return fmt.Errorf("ssh from login to worker as %s: %w", userName, err)
	}

	if !strings.Contains(out, worker.Name) {
		return fmt.Errorf("unexpected ssh output %q, expected hostname %q", strings.TrimSpace(out), worker.Name)
	}

	return nil
}
