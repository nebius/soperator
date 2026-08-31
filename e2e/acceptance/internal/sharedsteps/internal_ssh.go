package sharedsteps

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const sshUserName = "bob"

type InternalSSH struct {
	runtime      framework.Runtime
	selector     *framework.WorkerSelector
	targetWorker framework.WorkerInfo
	sshOutput    string
}

func NewInternalSSH(runtime framework.Runtime, selector *framework.WorkerSelector) *InternalSSH {
	return &InternalSSH{runtime: runtime, selector: selector}
}

func (s *InternalSSH) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a regular user account exists on the login node$`, s.aRegularUserAccountExistsOnTheLoginNode)
	sc.Step(`^the user SSHs from the login node to a worker$`, s.theUserSSHsFromTheLoginNodeToAWorker)
	sc.Step(`^the connection succeeds without extra SSH options$`, s.theConnectionSucceedsWithoutExtraSSHOptions)
}

func (s *InternalSSH) CleanupAndReset(ctx context.Context) {
	s.targetWorker = framework.WorkerInfo{}
	s.sshOutput = ""
}

func (s *InternalSSH) aRegularUserAccountExistsOnTheLoginNode(ctx context.Context) error {
	workers, err := s.selector.PickWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.targetWorker = workers[0]

	return ensureSSHTestUser(ctx, s.runtime, sshUserName)
}

func (s *InternalSSH) theUserSSHsFromTheLoginNodeToAWorker(ctx context.Context) error {
	worker := framework.ShellQuote(s.targetWorker.Name)
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
	out, err := s.runtime.Jail().RunWithDefaultRetry(ctx, cmd)
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
