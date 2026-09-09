package slurmapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClient_GetDiag_LargeScheduleCycleSum pins the 64-bit decode path for sdiag statistics.
// schedule_cycle_sum grows monotonically (microseconds of cumulative scheduler cycle time) and
// exceeds int32 after ~35.8 min and uint32 after ~71.6 min on a busy controller; older bindings
// declared it as int32 and the whole diag unmarshal failed. The fixture value is above 2^32, so
// a regression to any 32-bit type fails this test through the real generated JSON decode path,
// which mockery-based tests bypass.
func TestClient_GetDiag_LargeScheduleCycleSum(t *testing.T) {
	payload, err := os.ReadFile("testdata/sdiag_rest.json")
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/slurm/v0.0.44/diag/", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	c, err := NewClient(server.URL, nil, server.Client())
	require.NoError(t, err)

	diag, err := c.GetDiag(context.Background())
	require.NoError(t, err)
	require.NotNil(t, diag)

	require.NotNil(t, diag.Statistics.ScheduleCycleSum)
	assert.Equal(t, int64(6_442_450_944), *diag.Statistics.ScheduleCycleSum)

	require.NotNil(t, diag.Statistics.ServerThreadCount)
	assert.Equal(t, int32(3), *diag.Statistics.ServerThreadCount)

	require.NotNil(t, diag.Statistics.RpcsByMessageType)
	require.Len(t, *diag.Statistics.RpcsByMessageType, 2)
	assert.Equal(t, int64(6_871_947_674), (*diag.Statistics.RpcsByMessageType)[0].TotalTime)

	require.NotNil(t, diag.Statistics.RpcsByUser)
	require.Len(t, *diag.Statistics.RpcsByUser, 1)
	assert.Equal(t, int64(7_384_185_912), (*diag.Statistics.RpcsByUser)[0].TotalTime)
}
