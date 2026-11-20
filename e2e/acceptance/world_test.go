package acceptance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func stubKubectl(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	t.Setenv("E2E_TEST_KUBECTL_CALLS", calls)
	t.Setenv("E2E_TEST_KUBECTL_STATE", filepath.Join(dir, "state"))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte("#!/bin/sh\nset -eu\nprintf '%s\\n' \"$*\" >> \"$E2E_TEST_KUBECTL_CALLS\"\n"+script), 0o700))
	return calls
}

func TestJailRecoversAfterCachedLoginPodDisappears(t *testing.T) {
	calls := stubKubectl(t, `
case "$1" in
exec)
    if [ "$4" = login-0 ]; then
        echo 'error: unable to upgrade connection: container not found ("sshd")' >&2
        exit 1
    fi
    echo executed
    ;;
get)
    if [ ! -f "$E2E_TEST_KUBECTL_STATE" ]; then
        touch "$E2E_TEST_KUBECTL_STATE"
        echo '{"items":[]}'
    else
        echo '{"items":[{"metadata":{"name":"login-1"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}]}'
    fi
    ;;
*) exit 2 ;;
esac
`)
	synctest.Test(t, func(t *testing.T) {
		w := &world{slurmClusterName: "soperator", loginPodName: "login-0"}
		output, err := w.Jail().Run(t.Context(), "deluser --remove-home test-user")
		require.NoError(t, err)
		assert.Equal(t, "executed\n", output)
		assert.Equal(t, "login-1", w.loginPodName)
		data, err := os.ReadFile(calls)
		require.NoError(t, err)
		assert.Equal(t, 1, strings.Count(string(data), "exec -n soperator login-0 -- chroot /mnt/jail bash -lc deluser"))
		assert.Equal(t, 1, strings.Count(string(data), "exec -n soperator login-1 -- chroot /mnt/jail bash -lc deluser"))
		assert.Equal(t, 2, strings.Count(string(data), "get pods"))
	})
}

func TestWorkloadPodScopeDoesNotReplayCommandsWithUncertainOrRealFailures(t *testing.T) {
	for _, stderr := range []string{
		"sacctmgr: error: connection refused\ncommand terminated with exit code 1",
		"error: unexpected EOF",
		"error: unable to upgrade connection: container not found\ncommand terminated with exit code 1",
	} {
		t.Run(stderr, func(t *testing.T) {
			calls := stubKubectl(t, "printf '%s\\n' "+framework.ShellQuote(stderr)+" >&2\nexit 1\n")
			w := &world{slurmClusterName: "soperator", loginPodName: "login-0"}
			_, err := w.Jail().Run(t.Context(), "sacctmgr -i add user test-user")
			require.Error(t, err)
			var command *framework.CommandError
			require.ErrorAs(t, err, &command)
			assert.Equal(t, stderr, command.Stderr)
			assert.Equal(t, "login-0", w.loginPodName)
			data, err := os.ReadFile(calls)
			require.NoError(t, err)
			assert.Len(t, strings.Split(strings.TrimSpace(string(data)), "\n"), 1)
		})
	}
}

func TestJailWaitsForReplacementWithTheSamePodName(t *testing.T) {
	stubKubectl(t, `
if [ "$1" = get ]; then
    echo '{"items":[{"metadata":{"name":"login-0","uid":"replacement"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}]}'
elif [ ! -f "$E2E_TEST_KUBECTL_STATE" ]; then
    touch "$E2E_TEST_KUBECTL_STATE"
    echo 'Error from server (NotFound): pods "login-0" not found' >&2
    exit 1
else
    echo restored
fi
`)
	synctest.Test(t, func(t *testing.T) {
		w := &world{slurmClusterName: "soperator", loginPodName: "login-0"}
		output, err := w.Jail().Run(t.Context(), "getent passwd test-user")
		require.NoError(t, err)
		assert.Equal(t, "restored\n", output)
		assert.Equal(t, "login-0", w.loginPodName)
	})
}

func TestJailReadinessRecoveryIsBoundedAndHonorsCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprint(canceled), func(t *testing.T) {
			stubKubectl(t, "echo '{\"items\":[]}'\n")
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if canceled {
					time.AfterFunc(time.Second, cancel)
				}
				start := time.Now()
				_, err := (&world{slurmClusterName: "soperator"}).Jail().Run(ctx, "sacct")
				if canceled {
					require.ErrorIs(t, err, context.Canceled)
					assert.Equal(t, time.Second, time.Since(start))
				} else {
					require.ErrorContains(t, err, "no Ready login pods")
					assert.Equal(t, workloadPodTimeout, time.Since(start))
				}
			})
		})
	}
}

func TestCommandErrorSeparatesStderrAndPreservesExitStatus(t *testing.T) {
	w := &world{}
	_, err := w.Run(t.Context(), "sh", "-c", "printf 'connection refused' >&2; exit 7", "invalid timeout payload")
	var command *framework.CommandError
	require.ErrorAs(t, err, &command)
	assert.Equal(t, "connection refused", command.Stderr)
	assert.Contains(t, command.Args, "invalid timeout payload")
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 7, exit.ExitCode())
}

func TestKubectlArgsAddsContext(t *testing.T) {
	assert.Equal(t,
		[]string{"--context", "dev-context", "get", "pods"},
		kubectlArgs("dev-context", []string{"get", "pods"}),
	)
}

func TestKubectlArgsWithoutContext(t *testing.T) {
	assert.Equal(t,
		[]string{"get", "pods"},
		kubectlArgs("", []string{"get", "pods"}),
	)
}

func TestWaitForHonorsPermanentErrors(t *testing.T) {
	original := errors.New("worker safety violation")
	for _, tt := range []struct {
		name      string
		err       error
		permanent bool
	}{
		{"ordinary error", original, false},
		{"permanent error", &framework.PermanentError{Err: original}, true},
		{"wrapped permanent error", fmt.Errorf("observe worker: %w", &framework.PermanentError{Err: original}), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var calls int
				err := (&world{}).WaitFor(t.Context(), "worker recovery", time.Minute, time.Second,
					func(context.Context) (bool, error) {
						calls++
						if calls == 1 {
							return false, tt.err
						}
						return true, nil
					})
				if tt.permanent {
					require.ErrorIs(t, err, original)
					var permanent *framework.PermanentError
					assert.ErrorAs(t, err, &permanent)
					assert.ErrorContains(t, err, "wait for worker recovery")
					assert.Equal(t, 1, calls)
				} else {
					require.NoError(t, err)
					assert.Equal(t, 2, calls)
				}
			})
		})
	}
}

func TestWaitForContextTakesPrecedenceOverLateResults(t *testing.T) {
	original := errors.New("worker safety violation")
	pending := errors.New("replacement pending: original host=worker-0-host")
	for _, tt := range []struct {
		name       string
		finalErr   error
		pending    bool
		cancelWait bool
	}{
		{name: "permanent error after timeout", finalErr: &framework.PermanentError{Err: original}},
		{name: "timeout preserves pending state", finalErr: &framework.PermanentError{Err: original}, pending: true},
		{name: "success after timeout"},
		{name: "permanent error after cancellation", finalErr: &framework.PermanentError{Err: original}, cancelWait: true},
		{name: "cancellation with pending state", finalErr: &framework.PermanentError{Err: original}, pending: true, cancelWait: true},
		{name: "success after cancellation", cancelWait: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if tt.cancelWait {
					time.AfterFunc(2*time.Second, cancel)
				}
				var calls int
				err := (&world{}).WaitFor(ctx, "worker recovery", time.Minute, time.Second,
					func(waitCtx context.Context) (bool, error) {
						calls++
						if calls == 1 && tt.pending {
							return false, pending
						}
						<-waitCtx.Done()
						return true, tt.finalErr
					})

				require.Error(t, err)
				assert.NotErrorIs(t, err, original)
				switch {
				case tt.cancelWait:
					assert.ErrorIs(t, err, context.Canceled)
				case tt.pending:
					assert.ErrorIs(t, err, pending)
				default:
					assert.ErrorContains(t, err, "timed out after 1m0s")
				}
			})
		})
	}
}
