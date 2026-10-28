package slurmapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetDiag_RPCCountOverflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/slurm/v0.0.41/diag/", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"statistics":{"rpcs_by_message_type":[{"message_type":"REQUEST_JOB_INFO","count":4294967295,"total_time":6871947674}],"rpcs_by_user":[{"user":"nebius","user_id":1000,"count":4294967293,"total_time":7384185912}]}}`))
	}))
	defer server.Close()

	c, err := NewClient(server.URL, nil, server.Client())
	require.NoError(t, err)

	diag, err := c.GetDiag(context.Background())
	require.NoError(t, err)
	require.NotNil(t, diag)
	require.NotNil(t, diag.Statistics.RpcsByMessageType)
	require.Len(t, *diag.Statistics.RpcsByMessageType, 1)
	assert.Equal(t, int64(4_294_967_295), (*diag.Statistics.RpcsByMessageType)[0].Count)
	require.NotNil(t, diag.Statistics.RpcsByUser)
	require.Len(t, *diag.Statistics.RpcsByUser, 1)
	assert.Equal(t, int64(4_294_967_293), (*diag.Statistics.RpcsByUser)[0].Count)
	assert.Equal(t, int32(1000), (*diag.Statistics.RpcsByUser)[0].UserId)
}
