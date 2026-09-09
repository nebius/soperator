package slurmapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func undrainJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}
}

func TestUndrainNodePostsV0044PayloadAndHeaders(t *testing.T) {
	var gotBody map[string]any
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/slurm/v0.0.44/node/worker-1", request.URL.Path)
		assert.Equal(t, headerApplicationJson, request.Header.Get(headerContentType))
		assert.Equal(t, "token-value", request.Header.Get(headerSlurmUserToken))

		require.NoError(t, json.NewDecoder(request.Body).Decode(&gotBody))
		return undrainJSONResponse(http.StatusOK, `{"errors":[]}`), nil
	})}

	client, err := NewClient("http://slurmrestd/", staticTokenIssuer("token-value"), httpClient)
	require.NoError(t, err)

	require.NoError(t, client.UndrainNode(context.Background(), "worker-1"))
	assert.Equal(t, []any{"UNDRAIN"}, gotBody["state"])
}

func TestUndrainNodeRejectsEmptyNodeName(t *testing.T) {
	client, err := NewClient("http://slurmrestd", nil, nil)
	require.NoError(t, err)

	err = client.UndrainNode(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node name is required")
}

func TestUndrainNodeStatusErrorSummarizesSlurmEnvelope(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return undrainJSONResponse(http.StatusUnprocessableEntity, `{"errors":[{"description":"Invalid node state"}]}`), nil
	})}

	client, err := NewClient("http://slurmrestd", nil, httpClient)
	require.NoError(t, err)

	err = client.UndrainNode(context.Background(), "worker-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status=422")
	assert.Contains(t, err.Error(), "Invalid node state")
}

func TestUndrainNodeDetectsSlurmErrorsOnOK(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return undrainJSONResponse(http.StatusOK, `{"errors":[{"error":"ESLURM_INVALID_NODE_STATE"}]}`), nil
	})}

	client, err := NewClient("http://slurmrestd", nil, httpClient)
	require.NoError(t, err)

	err = client.UndrainNode(context.Background(), "worker-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ESLURM_INVALID_NODE_STATE")
}

func TestUndrainNodeAcceptsEmptySuccessResponse(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := undrainJSONResponse(http.StatusOK, "")
		response.Header.Del("Content-Type")
		return response, nil
	})}

	client, err := NewClient("http://slurmrestd", nil, httpClient)
	require.NoError(t, err)
	require.NoError(t, client.UndrainNode(context.Background(), "worker-1"))
}
