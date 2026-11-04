package sharedsteps

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type sshTestUserRuntime struct {
	framework.Runtime
	jail        framework.CommandScope
	controller  framework.CommandScope
	worker      framework.CommandScope
	description string
}

func (r *sshTestUserRuntime) Jail() framework.CommandScope {
	return r.jail
}

func (r *sshTestUserRuntime) Controller() framework.CommandScope {
	return r.controller
}

func (r *sshTestUserRuntime) Worker(framework.WorkerInfo) framework.CommandScope {
	return r.worker
}

func (r *sshTestUserRuntime) WaitFor(
	ctx context.Context,
	description string,
	_, _ time.Duration,
	condition func(context.Context) (bool, error),
) error {
	r.description = description
	done, err := condition(ctx)
	if err != nil {
		return err
	}
	if !done {
		return errors.New("identity is not ready")
	}
	return nil
}

func TestRunInternalSSHAsTestUser(t *testing.T) {
	var command string
	runtime := &sshTestUserRuntime{
		jail: framework.NewCommandScope(func(_ context.Context, value string) (string, error) {
			command = value
			return "worker-0\n", nil
		}),
	}

	output, err := runInternalSSHAsTestUser(t.Context(), runtime, "worker-0", 30*time.Second, "ulimit -n")
	require.NoError(t, err)
	assert.Equal(t, "worker-0\n", output)
	assert.Equal(t, "su - 'bob' -c "+framework.ShellQuote(
		"mkdir -p ~/.ssh && touch ~/.ssh/known_hosts && "+
			"(ssh-keygen -R 'worker-0' -f ~/.ssh/known_hosts >/dev/null 2>&1 || true) && "+
			"timeout 30 ssh 'worker-0' 'ulimit -n' </dev/null",
	), command)
}

func TestWaitForSSHTestUserOnController(t *testing.T) {
	var command string
	runtime := &sshTestUserRuntime{
		controller: framework.NewCommandScope(func(_ context.Context, value string) (string, error) {
			command = value
			return "", nil
		}),
	}

	require.NoError(t, waitForSSHTestUserOnController(t.Context(), runtime, "bob"))
	assert.Equal(t, "SSH test user bob visible on controller", runtime.description)
	assert.Equal(t, "getent passwd 'bob' >/dev/null", command)
}

func TestWaitForSSHTestUserInGroupOnWorkerIncludesDiagnostics(t *testing.T) {
	var commands []string
	runtime := &sshTestUserRuntime{
		worker: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
			commands = append(commands, command)
			if len(commands) == 1 {
				return "", errors.New("exit status 1")
			}
			return "id alice:\nuid=1000(alice) gid=1000(alice) groups=1000(alice)\n" +
				"getent passwd alice:\nalice:x:1000:1000::/home/alice:/bin/bash\n" +
				"getent group containers:\ncontainers:x:999:", nil
		}),
	}

	err := waitForSSHTestUserInGroupOnWorker(
		t.Context(),
		runtime,
		"alice",
		"containers",
		framework.WorkerInfo{Name: "worker-0"},
	)
	require.Error(t, err)
	assert.Equal(t, "SSH test user alice in group containers on worker worker-0", runtime.description)
	require.Len(t, commands, 2)
	assert.Equal(t,
		"id -nG 'alice' | tr '[:space:]' '\\n' | grep -Fx -- 'containers' >/dev/null",
		commands[0],
	)
	assert.Contains(t, commands[1], "id 'alice' 2>&1 || true")
	assert.Contains(t, commands[1], "getent passwd 'alice' 2>&1 || true")
	assert.Contains(t, commands[1], "getent group 'containers' 2>&1 || true")
	assert.ErrorContains(t, err, "identity diagnostics:")
	assert.ErrorContains(t, err, "groups=1000(alice)")
	assert.ErrorContains(t, err, "containers:x:999:")
}

func TestWaitForSSHTestUserInGroupOnWorkerValidatesTarget(t *testing.T) {
	runtime := &sshTestUserRuntime{}

	err := waitForSSHTestUserInGroupOnWorker(t.Context(), runtime, "dockeruser", "docker", framework.WorkerInfo{})
	assert.EqualError(t, err, "wait for SSH test user dockeruser in group docker: worker name is empty")

	err = waitForSSHTestUserInGroupOnWorker(
		t.Context(), runtime, "dockeruser", " ", framework.WorkerInfo{Name: "worker-0"},
	)
	assert.EqualError(t, err, "wait for SSH test user dockeruser on worker worker-0: required group is empty")
}
