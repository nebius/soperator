package sharedsteps

import (
	"context"
	"fmt"
	"time"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const sshTestUserPropagationTimeout = time.Minute

func ensureSSHTestUser(ctx context.Context, runtime framework.Runtime, userName string) error {
	quotedUserName := framework.ShellQuote(userName)
	command := fmt.Sprintf(
		"id %s >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh %s",
		quotedUserName,
		quotedUserName,
	)
	if _, err := runtime.Jail().Run(ctx, command); err != nil {
		return fmt.Errorf("create SSH test user %s: %w", userName, err)
	}

	return nil
}

func waitForSSHTestUserOnWorker(
	ctx context.Context,
	runtime framework.Runtime,
	userName string,
	worker framework.WorkerInfo,
) error {
	if worker.Name == "" {
		return fmt.Errorf("wait for SSH test user %s: worker name is empty", userName)
	}
	command := "getent passwd " + framework.ShellQuote(userName) + " >/dev/null"
	return runtime.WaitFor(ctx,
		fmt.Sprintf("SSH test user %s visible on worker %s", userName, worker.Name),
		sshTestUserPropagationTimeout,
		framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			if _, err := runtime.Worker(worker).Run(waitCtx, command); err != nil {
				return false, err
			}
			return true, nil
		},
	)
}
