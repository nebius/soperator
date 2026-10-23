package sharedsteps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const internalSSHTimeout = 30 * time.Second

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

	if err := ensureSSHTestUser(ctx, s.runtime, sshTestUserName); err != nil {
		return err
	}
	return waitForSSHTestUserOnWorker(ctx, s.runtime, sshTestUserName, s.targetWorker)
}

func (s *InternalSSH) theUserSSHsFromTheLoginNodeToAWorker(ctx context.Context) error {
	out, err := runInternalSSHAsTestUser(ctx, s.runtime, s.targetWorker.Name, internalSSHTimeout, "hostname")
	if err != nil {
		return fmt.Errorf("ssh from login to worker as %s: %w", sshTestUserName, err)
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
