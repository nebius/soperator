package slurmapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetNodeTopologyPostsTheRegistration(t *testing.T) {
	var gotBody map[string]any
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, request.Method)
		// One request covers every node sharing the registration, so the path carries a hostlist.
		assert.Equal(t, "/slurm/v0.0.44/node/worker-[0-3]", request.URL.Path)

		require.NoError(t, json.NewDecoder(request.Body).Decode(&gotBody))
		return undrainJSONResponse(http.StatusOK, `{"errors":[]}`), nil
	})}

	client, err := NewClient("http://slurmrestd/", staticTokenIssuer("token-value"), httpClient)
	require.NoError(t, err)

	require.NoError(t, client.SetNodeTopology(
		context.Background(), "worker-[0-3]", "tree-ib:fab:leaf-a"))
	assert.Equal(t, "tree-ib:fab:leaf-a", gotBody["topology_str"])
}

func TestSetNodeTopologyRejectsAnEmptyNodeExpression(t *testing.T) {
	client, err := NewClient("http://slurmrestd", nil, nil)
	require.NoError(t, err)

	assert.ErrorContains(t,
		client.SetNodeTopology(context.Background(), "", "tree-ib:fab:leaf-a"),
		"node expression is required")
}

// Slurm reports an unloaded topology as a plain rejection, but for the operator it means "not yet":
// the reconfigure that would load it has not landed. It has to be distinguishable so the caller
// retries instead of reporting a failure.
func TestSetNodeTopologyReportsAnUnloadedTopology(t *testing.T) {
	const envelope = `{"errors":[{"error":"ESLURM_REQUESTED_TOPO_CONFIG_UNAVAILABLE",` +
		`"description":"Requested topology configuration is not available","error_number":2178}]}`

	for name, status := range map[string]int{
		"rejected outright":      http.StatusBadRequest,
		"reported inside the OK": http.StatusOK,
	} {
		t.Run(name, func(t *testing.T) {
			httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return undrainJSONResponse(status, envelope), nil
			})}
			client, err := NewClient("http://slurmrestd", nil, httpClient)
			require.NoError(t, err)

			err = client.SetNodeTopology(context.Background(), "worker-0", "tree-ib:fab:leaf-a")
			assert.ErrorIs(t, err, ErrTopologyUnavailable)
		})
	}
}

func TestSetNodeTopologyOtherErrorsAreNotRetryable(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return undrainJSONResponse(http.StatusUnprocessableEntity,
			`{"errors":[{"error":"ESLURM_INVALID_NODE_NAME","description":"Invalid node name"}]}`), nil
	})}

	client, err := NewClient("http://slurmrestd", nil, httpClient)
	require.NoError(t, err)

	err = client.SetNodeTopology(context.Background(), "worker-0", "tree-ib:fab:leaf-a")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrTopologyUnavailable)
}

func TestSetNodeTopologyAcceptsEmptySuccessResponse(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		// Without a JSON content type the generated client leaves the body unparsed, which is how
		// slurmrestd answers an update it had nothing to say about.
		response := undrainJSONResponse(http.StatusOK, "")
		response.Header.Del("Content-Type")
		return response, nil
	})}

	client, err := NewClient("http://slurmrestd", nil, httpClient)
	require.NoError(t, err)

	assert.NoError(t, client.SetNodeTopology(context.Background(), "worker-0", "tree-ib:fab:leaf-a"))
}
