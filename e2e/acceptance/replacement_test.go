package acceptance

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type instanceRemovalTestRuntime struct {
	*world
	local framework.ArgsScope
}

func (r *instanceRemovalTestRuntime) Local() framework.ArgsScope {
	return r.local
}

func TestWaitForInstanceRemovalWithRealPolling(t *testing.T) {
	notFound := errors.New("rpc error: code = NotFound desc = instance not found")
	for _, tt := range []struct {
		name         string
		lookupErr    error
		lookupOutput string
		wantError    bool
		wantCalls    int
	}{
		{name: "instance still exists"},
		{name: "permission denied", lookupErr: errors.New("rpc error: code = PermissionDenied desc = permission denied"), wantError: true},
		{name: "unauthenticated", lookupErr: errors.New("rpc error: code = Unauthenticated desc = invalid credentials"), wantError: true},
		{name: "case insensitive permission denied", lookupErr: errors.New("rpc error: code = PERMISSIONDENIED desc = permission denied"), wantError: true},
		{name: "case insensitive not found", lookupErr: errors.New("rpc error: code = NOTFOUND desc = instance not found"), wantCalls: 1},
		{name: "temporary API unavailable", lookupErr: errors.New("rpc error: code = Unavailable desc = connection reset")},
		{name: "temporary API internal error", lookupErr: errors.New("rpc error: code = Internal desc = temporary failure")},
		{name: "network reset", lookupErr: errors.New("connection reset by peer")},
		{name: "CLI command timeout", lookupErr: errors.New("nebius compute instance get: signal: killed")},
		{name: "individual request deadline", lookupErr: context.DeadlineExceeded},
		{name: "non-API permission failure", lookupErr: errors.New("SSH: Permission denied (publickey)")},
		{name: "missing configuration", lookupErr: errors.New("configuration file not found")},
		{name: "misleading API description", lookupErr: errors.New("rpc error: code = PermissionDenied desc = credentials not found"), wantError: true},
		{name: "nested status in description", lookupErr: errors.New("rpc error: code = PermissionDenied desc = rpc error: code = NotFound desc = credentials not found"), wantError: true},
		{name: "different RPC status", lookupErr: errors.New("rpc error: code = NotFoundElsewhere desc = instance not found")},
		{name: "malformed response", lookupOutput: "{"},
		{name: "empty response", lookupOutput: " "},
		{name: "missing identity", lookupOutput: `{}`},
		{name: "wrong identity", lookupOutput: `{"metadata":{"id":"another-instance"}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				lookups := []error{tt.lookupErr, notFound}
				var calls int
				runtime := &instanceRemovalTestRuntime{
					world: &world{},
					local: framework.NewArgsScope(func(_ context.Context, _ ...string) (string, error) {
						require.Less(t, calls, len(lookups))
						err := lookups[calls]
						calls++
						output := tt.lookupOutput
						if output == "" {
							output = `{"metadata":{"id":"old-instance"}}`
						}
						return output, err
					}),
				}

				err := framework.WaitForInstanceRemoval(t.Context(), runtime, "old-instance", time.Minute, time.Second)
				if tt.wantError {
					require.ErrorIs(t, err, framework.ErrComputeInstanceAccess)
					if tt.lookupErr != nil {
						assert.ErrorIs(t, err, tt.lookupErr)
					}
					assert.ErrorContains(t, err, "old instance removal")
					assert.ErrorContains(t, err, "old-instance")
					assert.Equal(t, 1, calls)
				} else {
					require.NoError(t, err)
					wantCalls := tt.wantCalls
					if wantCalls == 0 {
						wantCalls = 2
					}
					assert.Equal(t, wantCalls, calls)
				}
			})
		})
	}
}

func TestWaitForInstanceRemovalPreservesDeadlineAndCancellation(t *testing.T) {
	killed := errors.New("nebius compute instance get: signal: killed")
	denied := errors.New("rpc error: code = PermissionDenied desc = permission denied")
	pending := errors.New("rpc error: code = Unavailable desc = temporary failure")
	for _, tt := range []struct {
		name        string
		finalErr    error
		pendingErr  error
		cancelAfter time.Duration
	}{
		{name: "killed at wait deadline", finalErr: killed},
		{name: "access error at wait deadline", finalErr: denied},
		{name: "preserve last pending lookup", pendingErr: pending, finalErr: killed},
		{name: "caller cancellation", finalErr: denied, cancelAfter: 2 * time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if tt.cancelAfter > 0 {
					time.AfterFunc(tt.cancelAfter, cancel)
				}
				var calls int
				runtime := &instanceRemovalTestRuntime{
					world: &world{},
					local: framework.NewArgsScope(func(ctx context.Context, _ ...string) (string, error) {
						calls++
						if calls == 1 && tt.pendingErr != nil {
							return "", tt.pendingErr
						}
						<-ctx.Done()
						return "", tt.finalErr
					}),
				}

				err := framework.WaitForInstanceRemoval(ctx, runtime, "old-instance", time.Minute, time.Second)

				require.Error(t, err)
				assert.NotErrorIs(t, err, framework.ErrComputeInstanceAccess)
				assert.NotErrorIs(t, err, tt.finalErr)
				switch {
				case tt.cancelAfter > 0:
					assert.ErrorIs(t, err, context.Canceled)
				case tt.pendingErr != nil:
					assert.ErrorIs(t, err, tt.pendingErr)
					assert.Equal(t, 2, calls)
				default:
					assert.ErrorContains(t, err, "wait for old instance removal: timed out after 1m0s")
					assert.Equal(t, 1, calls)
				}
			})
		})
	}
}
