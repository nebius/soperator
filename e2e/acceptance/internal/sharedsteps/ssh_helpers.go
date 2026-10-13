package sharedsteps

import (
	"context"
	"fmt"
	"strings"
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
	return waitForSSHTestIdentity(ctx, runtime, runtime.Worker(worker),
		fmt.Sprintf("SSH test user %s visible on worker %s", userName, worker.Name),
		"getent passwd "+framework.ShellQuote(userName)+" >/dev/null",
		userName,
		"",
	)
}

func waitForSSHTestUserOnController(
	ctx context.Context,
	runtime framework.Runtime,
	userName string,
) error {
	return waitForSSHTestIdentity(ctx, runtime, runtime.Controller(),
		fmt.Sprintf("SSH test user %s visible on controller", userName),
		"getent passwd "+framework.ShellQuote(userName)+" >/dev/null",
		userName,
		"",
	)
}

func waitForSSHTestUserInGroupOnWorker(
	ctx context.Context,
	runtime framework.Runtime,
	userName string,
	groupName string,
	worker framework.WorkerInfo,
) error {
	if worker.Name == "" {
		return fmt.Errorf("wait for SSH test user %s in group %s: worker name is empty", userName, groupName)
	}
	if strings.TrimSpace(groupName) == "" {
		return fmt.Errorf("wait for SSH test user %s on worker %s: required group is empty", userName, worker.Name)
	}
	command := fmt.Sprintf(
		"id -nG %s | tr '[:space:]' '\\n' | grep -Fx -- %s >/dev/null",
		framework.ShellQuote(userName),
		framework.ShellQuote(groupName),
	)
	return waitForSSHTestIdentity(ctx, runtime, runtime.Worker(worker),
		fmt.Sprintf("SSH test user %s in group %s on worker %s", userName, groupName, worker.Name),
		command,
		userName,
		groupName,
	)
}

func waitForSSHTestIdentity(
	ctx context.Context,
	runtime framework.Runtime,
	scope framework.CommandScope,
	description string,
	readinessCommand string,
	userName string,
	groupName string,
) error {
	return runtime.WaitFor(ctx,
		description,
		sshTestUserPropagationTimeout,
		framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			if _, err := scope.Run(waitCtx, readinessCommand); err != nil {
				diagnostics, diagnosticsErr := scope.Run(
					waitCtx,
					sshTestIdentityDiagnosticsCommand(userName, groupName),
				)
				if diagnosticsErr != nil {
					return false, fmt.Errorf(
						"check %s: %w; collect identity diagnostics: %v",
						description,
						err,
						diagnosticsErr,
					)
				}
				diagnostics = strings.TrimSpace(diagnostics)
				if diagnostics == "" {
					diagnostics = "<empty>"
				}
				return false, fmt.Errorf(
					"check %s: %w; identity diagnostics:\n%s",
					description,
					err,
					diagnostics,
				)
			}
			return true, nil
		},
	)
}

func sshTestIdentityDiagnosticsCommand(userName, groupName string) string {
	quotedUserName := framework.ShellQuote(userName)
	commands := []string{
		"printf '%s\\n' " + framework.ShellQuote("id "+userName+":"),
		"id " + quotedUserName + " 2>&1 || true",
		"printf '%s\\n' " + framework.ShellQuote("getent passwd "+userName+":"),
		"getent passwd " + quotedUserName + " 2>&1 || true",
	}
	if groupName != "" {
		commands = append(commands,
			"printf '%s\\n' "+framework.ShellQuote("getent group "+groupName+":"),
			"getent group "+framework.ShellQuote(groupName)+" 2>&1 || true",
		)
	}
	return strings.Join(commands, "\n")
}

func ensureSSHTestIdentity(
	ctx context.Context,
	runtime framework.Runtime,
	userName,
	keyName,
	keyComment string,
) error {
	setupIdentity := fmt.Sprintf(`
set -euo pipefail
install -d -m 0700 "${HOME}/.ssh"
key="${HOME}/.ssh/%s"
authorized_keys="${HOME}/.ssh/authorized_keys"
rm -f "${key}" "${key}.pub"
touch "${authorized_keys}"
sed -i '\# %s$#d' "${authorized_keys}"
ssh-keygen -q -t ecdsa -N '' -C %s -f "${key}"
cat "${key}.pub" >> "${authorized_keys}"
chmod 0600 "${authorized_keys}"
`, keyName, keyComment, framework.ShellQuote(keyComment))
	command := fmt.Sprintf(
		"su - %s -c %s",
		framework.ShellQuote(userName),
		framework.ShellQuote(framework.BashLC(setupIdentity)),
	)
	if _, err := runtime.Jail().Run(ctx, command); err != nil {
		return fmt.Errorf("prepare SSH identity for %s: %w", userName, err)
	}

	return nil
}

func removeSSHTestIdentity(
	ctx context.Context,
	runtime framework.Runtime,
	userName,
	keyName,
	keyComment string,
) error {
	cleanupIdentity := fmt.Sprintf(`
key="${HOME}/.ssh/%s"
authorized_keys="${HOME}/.ssh/authorized_keys"
rm -f "${key}" "${key}.pub"
if [ -f "${authorized_keys}" ]; then
    sed -i '\# %s$#d' "${authorized_keys}"
fi
`, keyName, keyComment)
	command := fmt.Sprintf(
		"if id %s >/dev/null 2>&1; then su - %s -c %s; fi",
		framework.ShellQuote(userName),
		framework.ShellQuote(userName),
		framework.ShellQuote(framework.BashLC(cleanupIdentity)),
	)
	_, err := runtime.Jail().Run(ctx, command)
	return err
}

func runSSHCommand(
	ctx context.Context,
	runtime framework.Runtime,
	userName,
	keyName,
	host string,
	timeout time.Duration,
	remoteCommand string,
) (string, error) {
	sshCommand := fmt.Sprintf(
		"timeout %.0f ssh -i ~/.ssh/%s -o IdentitiesOnly=yes -o BatchMode=yes -o LogLevel=ERROR -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null %s %s",
		timeout.Seconds(),
		keyName,
		framework.ShellQuote(host),
		framework.ShellQuote(framework.BashLC(remoteCommand)),
	)
	command := fmt.Sprintf(
		"su - %s -c %s",
		framework.ShellQuote(userName),
		framework.ShellQuote(sshCommand),
	)

	return runtime.Jail().Run(ctx, command)
}
