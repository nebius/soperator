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

type staticTokenIssuer string

func (s staticTokenIssuer) Issue(context.Context) (string, error) {
	return string(s), nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func rebootJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestRebootNodesPostsV0044PayloadAndHeaders(t *testing.T) {
	var gotBody map[string]any
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, slurmV0044RebootNodesPath, request.URL.Path)
		assert.Equal(t, headerApplicationJson, request.Header.Get(headerContentType))
		assert.Equal(t, "token-value", request.Header.Get(headerSlurmUserToken))

		require.NoError(t, json.NewDecoder(request.Body).Decode(&gotBody))
		return rebootJSONResponse(http.StatusOK, `{"errors":[]}`), nil
	})}

	client, err := NewClient("http://slurmrestd/", staticTokenIssuer("token-value"), httpClient)
	require.NoError(t, err)

	err = client.RebootNodes(context.Background(), RebootNodesRequest{
		NodeList:    "worker-[1-2]",
		ASAP:        true,
		Force:       true,
		NextState:   []RebootNextState{RebootNextStateResume},
		Reason:      "rolling update",
		PowerAction: "restart-slurmd",
	})
	require.NoError(t, err)

	assert.Equal(t, "worker-[1-2]", gotBody["nodes"])
	assert.Equal(t, true, gotBody["asap"])
	assert.Equal(t, true, gotBody["force"])
	assert.Equal(t, []any{"RESUME"}, gotBody["next_state"])
	assert.Equal(t, "rolling update", gotBody["reason"])
	assert.Equal(t, "restart-slurmd", gotBody["power_action"])
}

func TestRebootNodesRejectsEmptyNodeList(t *testing.T) {
	client, err := NewClient("http://slurmrestd", nil, nil)
	require.NoError(t, err)

	err = client.RebootNodes(context.Background(), RebootNodesRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nodes field is required")
}

func TestRebootNodesStatusErrorSummarizesSlurmEnvelope(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return rebootJSONResponse(http.StatusUnprocessableEntity, `{"errors":[{"description":"Invalid next_state"}]}`), nil
	})}

	client, err := NewClient("http://slurmrestd", nil, httpClient)
	require.NoError(t, err)

	err = client.RebootNodes(context.Background(), RebootNodesRequest{NodeList: "worker-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status=422")
	assert.Contains(t, err.Error(), "Invalid next_state")
}

func TestRebootNodesDetectsSlurmErrorsOnOK(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return rebootJSONResponse(http.StatusOK, `{"errors":[{"error":"ESLURM_INVALID_NODE_STATE"}]}`), nil
	})}

	client, err := NewClient("http://slurmrestd", nil, httpClient)
	require.NoError(t, err)

	err = client.RebootNodes(context.Background(), RebootNodesRequest{NodeList: "worker-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ESLURM_INVALID_NODE_STATE")
}
