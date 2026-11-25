package sharedsteps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

type sshTestExitError int

func (e sshTestExitError) Error() string { return fmt.Sprintf("exit status %d", e) }
func (e sshTestExitError) ExitCode() int { return int(e) }

func TestAnnotateSSHLockError(t *testing.T) {
	commandErr := &framework.CommandError{Err: sshTestExitError(75)}
	err := annotateSSHLockError(commandErr, "SSH identity")
	assert.ErrorContains(t, err, "acquire SSH identity lock: timed out after 1m0s")
	assert.ErrorIs(t, err, commandErr)

	for _, otherErr := range []error{
		nil,
		&framework.CommandError{Err: sshTestExitError(1)},
		errors.New("exit status 75"),
	} {
		assert.Equal(t, otherErr, annotateSSHLockError(otherErr, "SSH identity"))
	}
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
		"mkdir -p ~/.ssh && flock --conflict-exit-code 75 --wait 60 ~/.ssh/.soperator-e2e-known-hosts.lock bash -ec "+framework.ShellQuote("touch ~/.ssh/known_hosts && "+
			"(ssh-keygen -R 'worker-0' -f ~/.ssh/known_hosts >/dev/null 2>&1 || true) && "+
			"timeout 30 ssh 'worker-0' 'ulimit -n' </dev/null"),
	), command)
}

func TestEnsureSSHTestUserLocksCompleteInitialization(t *testing.T) {
	var command string
	commandErr := errors.New("initialization failed")
	runtime := &sshTestUserRuntime{jail: framework.NewCommandScope(func(_ context.Context, value string) (string, error) {
		command = value
		return "", commandErr
	})}
	err := ensureSSHTestUser(t.Context(), runtime, "bob")
	require.ErrorIs(t, err, commandErr)
	assert.Equal(t, "flock --conflict-exit-code 75 --wait 60 '/etc/soperator-e2e-createuser.lock' bash -ec "+framework.ShellQuote(
		"id 'bob' >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh 'bob'"), command)
}

func TestSSHInitializationSerializesConcurrentCalls(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("shell concurrency tests require Linux utilities")
	}
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("shell concurrency tests require flock")
	}
	identityStubs := `su() { bash -c "$4"; }
id() { echo 1000; }
ssh-keygen() {
    local comment key
    while [ "$#" -gt 0 ]; do
        case "$1" in
            -C) comment="$2"; shift 2 ;;
            -f) key="$2"; shift 2 ;;
            *) shift ;;
        esac
    done
    echo "$comment" >> "$HOME/events"
    touch "$key"
    printf 'ssh-ed25519 generated %s\n' "$comment" > "$key.pub"
    if [ "$comment" = first ]; then
        touch "$HOME/entered"
        while [ ! -e "$HOME/release" ]; do sleep 0.01; done
    fi
}
export -f su id ssh-keygen
`
	const existingKeys = "ssh-ed25519 unrelated keep\nssh-ed25519 stale second\n"
	for _, tc := range []struct {
		name, stubs, events string
		initialKeys         string
		wantKeys            string
		run                 func(context.Context, framework.Runtime, int) error
	}{
		{
			name: "user creation",
			stubs: `id() { test -f "$HOME/account"; }
createuser() {
    echo create >> "$HOME/events"
    touch "$HOME/account" "$HOME/entered"
    while [ ! -e "$HOME/release" ]; do sleep 0.01; done
    echo ready >> "$HOME/events"
}
export -f id createuser
`,
			events: "create\nready\n",
			run: func(ctx context.Context, runtime framework.Runtime, _ int) error {
				return ensureSSHTestUser(ctx, runtime, "bob")
			},
		},
		{
			name: "known hosts across different workers",
			stubs: `su() { bash -c "$4"; }
timeout() { shift; "$@"; }
ssh-keygen() { echo "remove $2" >> "$HOME/events"; }
ssh() {
    echo "ssh $1" >> "$HOME/events"
    if [ "$1" = worker-0 ]; then
        touch "$HOME/entered"
        while [ ! -e "$HOME/release" ]; do sleep 0.01; done
    fi
    echo "finished $1" >> "$HOME/events"
}
export -f su timeout ssh-keygen ssh
`,
			events: "remove worker-0\nssh worker-0\nfinished worker-0\nremove worker-1\nssh worker-1\nfinished worker-1\n",
			run: func(ctx context.Context, runtime framework.Runtime, index int) error {
				_, err := runInternalSSHAsUser(ctx, runtime, "bob", fmt.Sprintf("worker-%d", index), time.Second, "hostname")
				return err
			},
		},
		{
			name:        "identity setup with different keys",
			stubs:       identityStubs,
			events:      "first\nsecond\n",
			initialKeys: existingKeys,
			wantKeys:    "ssh-ed25519 unrelated keep\nssh-ed25519 generated first\nssh-ed25519 generated second\n",
			run: func(ctx context.Context, runtime framework.Runtime, index int) error {
				name := "first"
				if index == 1 {
					name = "second"
				}
				return ensureSSHTestIdentity(ctx, runtime, "bob", name, name)
			},
		},
		{
			name:        "identity setup and removal",
			stubs:       identityStubs,
			events:      "first\n",
			initialKeys: existingKeys,
			wantKeys:    "ssh-ed25519 unrelated keep\nssh-ed25519 generated first\n",
			run: func(ctx context.Context, runtime framework.Runtime, index int) error {
				if index == 0 {
					return ensureSSHTestIdentity(ctx, runtime, "bob", "first", "first")
				}
				return removeSSHTestIdentity(ctx, runtime, "bob", "second", "second")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			authorizedKeys := filepath.Join(directory, ".ssh", "authorized_keys")
			if tc.initialKeys != "" {
				require.NoError(t, os.Mkdir(filepath.Dir(authorizedKeys), 0o700))
				require.NoError(t, os.WriteFile(authorizedKeys, []byte(tc.initialKeys), 0o600))
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			release := filepath.Join(directory, "release")
			var calls sync.WaitGroup
			defer func() {
				_ = os.WriteFile(release, nil, 0o600)
				calls.Wait()
			}()
			started := make(chan struct{}, 2)
			runtime := &sshTestUserRuntime{jail: framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
				// Redirect the shared jail lock into the fixture; never touch host accounts or /etc.
				command = strings.ReplaceAll(command, sshTestUserLockPath, filepath.Join(directory, "createuser.lock"))
				cmd := exec.CommandContext(ctx, "bash", "-c", tc.stubs+command)
				cmd.Env = append(os.Environ(), "HOME="+directory)
				cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
				if err := cmd.Start(); err != nil {
					return "", err
				}
				started <- struct{}{}
				return "", cmd.Wait()
			})}

			first := make(chan error, 1)
			calls.Go(func() { first <- tc.run(ctx, runtime, 0) })
			require.Eventually(t, func() bool {
				_, err := os.Stat(filepath.Join(directory, "entered"))
				return err == nil
			}, 5*time.Second, 10*time.Millisecond, "first operation must reach its pause point")
			<-started

			second := make(chan error, 1)
			calls.Go(func() { second <- tc.run(ctx, runtime, 1) })
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("second operation did not start")
			}
			select {
			case err := <-second:
				t.Fatalf("second operation returned before initialization finished: %v", err)
			case <-time.After(100 * time.Millisecond):
			}

			require.NoError(t, os.WriteFile(release, nil, 0o600))
			require.NoError(t, <-first)
			require.NoError(t, <-second)
			events, err := os.ReadFile(filepath.Join(directory, "events"))
			require.NoError(t, err)
			assert.Equal(t, tc.events, string(events))
			if tc.wantKeys != "" {
				keys, err := os.ReadFile(authorizedKeys)
				require.NoError(t, err)
				assert.Equal(t, tc.wantKeys, string(keys))
			}
		})
	}
}

func TestRunInternalSSHAsUser(t *testing.T) {
	var command string
	runtime := &sshTestUserRuntime{
		jail: framework.NewCommandScope(func(_ context.Context, value string) (string, error) {
			command = value
			return "worker-0\n", nil
		}),
	}

	_, err := runInternalSSHAsUser(t.Context(), runtime, pamSlurmAdoptLimitsUser, "worker-0", 30*time.Second, "ulimit -n")
	require.NoError(t, err)
	assert.Contains(t, command, "su - 'soperatoradoptlimits' -c ")
	assert.NotContains(t, command, "'bob'")
}

func TestWaitForSSHHolderJobNeedsBatchStart(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stdout string
		ready  bool
	}{
		{name: "running before batch start"},
		{name: "batch started", stdout: "SOPERATOR_E2E_SSH_HOLDER_STARTED\n", ready: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := framework.SbatchJob{ID: "42", StdoutPath: "/jobs/42.out"}
			runtime := &sshTestUserRuntime{
				jail: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
					switch {
					case strings.HasPrefix(command, "squeue "):
						return "RUNNING\n", nil
					case strings.Contains(command, job.StdoutPath):
						return tc.stdout, nil
					default:
						return "", nil
					}
				}),
			}
			err := waitForSSHHolderJob(t.Context(), runtime, framework.NewSlurmClient(runtime), job, time.Minute)
			if tc.ready {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestUnprivilegedSSHUserCreationAndExistingGroupRemoval(t *testing.T) {
	var commands []string
	runtime := &sshTestUserRuntime{jail: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		commands = append(commands, command)
		if command == "id -u 'soperatormema' && id -nG 'soperatormema'" {
			return "1002\nsoperatormema sudo docker\n", nil
		}
		if command == "id -nG 'soperatormema'" {
			return "soperatormema\n", nil
		}
		return "", nil
	})}
	require.NoError(t, ensureUnprivilegedSSHTestUser(t.Context(), runtime, "soperatormema"))
	require.Len(t, commands, 5)
	assert.Contains(t, commands[0], framework.ShellQuote(
		"id 'soperatormema' >/dev/null 2>&1 || printf '\\n' | createuser --without-external-ssh '--without-sudo' '--without-docker' 'soperatormema'"))
	assert.Equal(t, "gpasswd -d 'soperatormema' 'sudo'", commands[2])
	assert.Equal(t, "gpasswd -d 'soperatormema' 'docker'", commands[3])
	assert.Equal(t, "id -nG 'soperatormema'", commands[4])
}

func TestUnprivilegedSSHUserRejectsSystemUIDBeforeGroupMutation(t *testing.T) {
	var commands []string
	runtime := &sshTestUserRuntime{jail: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		commands = append(commands, command)
		return "0\nroot sudo docker\n", nil
	})}
	err := ensureUnprivilegedSSHTestUser(t.Context(), runtime, "soperatormema")
	require.ErrorContains(t, err, "require a regular SSH user")
	assert.Len(t, commands, 2)
}

func TestUnprivilegedSSHGroupsRejectsRemainingPrivileges(t *testing.T) {
	for _, groups := range [][]string{nil, {"regular", "sudo"}, {"regular", "docker"}, {"root"}} {
		require.Error(t, validateUnprivilegedSSHGroups(groups))
	}
	require.NoError(t, validateUnprivilegedSSHGroups([]string{"regular", "users"}))
	problem := errors.New("group database is read-only")
	runtime := &sshTestUserRuntime{jail: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		if command == "id -u 'soperatormema' && id -nG 'soperatormema'" {
			return "1002\nsoperatormema sudo\n", nil
		}
		if command == "gpasswd -d 'soperatormema' 'sudo'" {
			return "", problem
		}
		return "", nil
	})}
	require.ErrorIs(t, ensureUnprivilegedSSHTestUser(t.Context(), runtime, "soperatormema"), problem)
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
