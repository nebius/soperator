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
	controller  framework.CommandScope
	worker      framework.CommandScope
	description string
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
			return "id dockeruser:\nuid=1000(dockeruser) gid=1000(dockeruser) groups=1000(dockeruser)\n" +
				"getent passwd dockeruser:\ndockeruser:x:1000:1000::/home/dockeruser:/bin/bash\n" +
				"getent group docker:\ndocker:x:999:", nil
		}),
	}

	err := waitForSSHTestUserInGroupOnWorker(
		t.Context(),
		runtime,
		"dockeruser",
		"docker",
		framework.WorkerInfo{Name: "worker-0"},
	)
	require.Error(t, err)
	assert.Equal(t, "SSH test user dockeruser in group docker on worker worker-0", runtime.description)
	require.Len(t, commands, 2)
	assert.Equal(t,
		"id -nG 'dockeruser' | tr '[:space:]' '\\n' | grep -Fx -- 'docker' >/dev/null",
		commands[0],
	)
	assert.Contains(t, commands[1], "id 'dockeruser' 2>&1 || true")
	assert.Contains(t, commands[1], "getent passwd 'dockeruser' 2>&1 || true")
	assert.Contains(t, commands[1], "getent group 'docker' 2>&1 || true")
	assert.ErrorContains(t, err, "identity diagnostics:")
	assert.ErrorContains(t, err, "groups=1000(dockeruser)")
	assert.ErrorContains(t, err, "docker:x:999:")
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
