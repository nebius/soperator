package slurmapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

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

func TestClient_RequestTimeout(t *testing.T) {
	operations := []struct {
		name string
		call func(context.Context, Client) error
	}{
		{name: "list nodes", call: func(ctx context.Context, c Client) error {
			_, err := c.ListNodes(ctx)
			return err
		}},
		{name: "reboot nodes", call: func(ctx context.Context, c Client) error {
			return c.RebootNodes(ctx, RebootNodesRequest{NodeList: "worker-0"})
		}},
		{name: "undrain nodes", call: func(ctx context.Context, c Client) error {
			return c.UndrainNodes(ctx, []string{"worker-0", "worker-10"})
		}},
	}
	responses := []struct {
		name       string
		status     int
		retryAfter string
	}{
		{name: "stalled headers"},
		{name: "stalled success body", status: http.StatusOK},
		{name: "stalled error body", status: http.StatusServiceUnavailable},
		{name: "retry backoff", status: http.StatusServiceUnavailable, retryAfter: "3600"},
	}
	for _, operation := range operations {
		for _, response := range responses {
			t.Run(operation.name+"/"+response.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					if response.retryAfter != "" {
						w.Header().Set("Retry-After", response.retryAfter)
						w.WriteHeader(response.status)
						return
					}
					if response.status != 0 {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(response.status)
						_, _ = w.Write([]byte("{"))
						w.(http.Flusher).Flush()
					}
					select {
					case <-r.Context().Done():
					case <-ctx.Done():
					}
				}))
				t.Cleanup(server.Close)
				t.Cleanup(cancel)

				httpClient := DefaultHTTPClient()
				httpClient.Timeout = 100 * time.Millisecond
				c, err := NewClient(server.URL, nil, httpClient)
				require.NoError(t, err)

				err = operation.call(ctx, c)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.NoError(t, ctx.Err(), "request must stop before the caller's deadline")
			})
		}
	}
}

func TestClient_DefaultHasNoRequestTimeout(t *testing.T) {
	require.Zero(t, DefaultHTTPClient().Timeout)
	c, err := NewClient("http://slurmrestd", nil, nil)
	require.NoError(t, err)
	require.Zero(t, c.(*client).httpClient.Timeout)
}
