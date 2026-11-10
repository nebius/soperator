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
		name         string
		lookupErr    error
		lookupOutput string
		wantError    bool
	}{
		{name: "instance still exists"},
		{name: "permission denied", lookupErr: errors.New("rpc error: code = PermissionDenied desc = permission denied"), wantError: true},
		{name: "unauthenticated", lookupErr: errors.New("rpc error: code = Unauthenticated desc = invalid credentials"), wantError: true},
		{name: "case insensitive status", lookupErr: errors.New("rpc error: code = PERMISSIONDENIED desc = permission denied"), wantError: true},
		{name: "exhausted API retries", lookupErr: errors.New("rpc error: code = Unavailable desc = connection reset"), wantError: true},
		{name: "non-API permission failure", lookupErr: errors.New("SSH: Permission denied (publickey)"), wantError: true},
		{name: "missing configuration", lookupErr: errors.New("configuration file not found"), wantError: true},
		{name: "misleading API description", lookupErr: errors.New("rpc error: code = PermissionDenied desc = credentials not found"), wantError: true},
		{name: "nested status in description", lookupErr: errors.New("rpc error: code = PermissionDenied desc = rpc error: code = NotFound desc = credentials not found"), wantError: true},
		{name: "different RPC status", lookupErr: errors.New("rpc error: code = NotFoundElsewhere desc = instance not found"), wantError: true},
		{name: "malformed response", lookupOutput: "{", wantError: true},
		{name: "empty response", lookupOutput: " ", wantError: true},
		{name: "missing identity", lookupOutput: `{}`, wantError: true},
		{name: "wrong identity", lookupOutput: `{"metadata":{"id":"another-instance"}}`, wantError: true},
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
					require.ErrorIs(t, err, framework.ErrComputeInstanceLookup)
					if tt.lookupErr != nil {
						assert.ErrorIs(t, err, tt.lookupErr)
					}
					assert.ErrorContains(t, err, "old instance removal")
					assert.ErrorContains(t, err, "old-instance")
					assert.Equal(t, 1, calls)
				} else {
					require.NoError(t, err)
					assert.Equal(t, 2, calls)
				}
			})
		})
	}
}
