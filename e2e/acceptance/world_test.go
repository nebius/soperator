package acceptance

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

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
