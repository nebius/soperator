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
		name      string
		lookupErr error
		wantError bool
	}{
		{name: "instance still exists"},
		{name: "permission denied", lookupErr: errors.New("rpc error: code = PermissionDenied desc = permission denied"), wantError: true},
		{name: "unauthenticated", lookupErr: errors.New("rpc error: code = Unauthenticated desc = invalid credentials"), wantError: true},
		{name: "case insensitive status", lookupErr: errors.New("rpc error: code = PERMISSIONDENIED desc = permission denied"), wantError: true},
		{name: "temporary API failure", lookupErr: errors.New("rpc error: code = Unavailable desc = connection reset")},
		{name: "non-API permission failure", lookupErr: errors.New("SSH: Permission denied (publickey)")},
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
						return "", err
					}),
				}

				err := framework.WaitForInstanceRemoval(t.Context(), runtime, "old-instance", time.Minute, time.Second)
				if tt.wantError {
					require.ErrorIs(t, err, tt.lookupErr)
					assert.ErrorContains(t, err, "old instance removal")
					assert.Equal(t, 1, calls)
				} else {
					require.NoError(t, err)
					assert.Equal(t, 2, calls)
				}
			})
		})
	}
}
