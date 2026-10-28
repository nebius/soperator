package slurmapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type responseRoundTripFunc func(*http.Request) (*http.Response, error)

func (f responseRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type trackedResponseBody struct {
	io.Reader
	closed bool
}

func (b *trackedResponseBody) Close() error {
	b.closed = true
	return nil
}

func TestClient_OverflowReadsHTTPBehavior(t *testing.T) {
	operations := []struct {
		name string
		path string
		call func(context.Context, Client) error
	}{
		{"list jobs", "/slurm/v0.0.41/jobs/", func(ctx context.Context, c Client) error {
			_, err := c.ListJobs(ctx)
			return err
		}},
		{"get job", "/slurm/v0.0.41/job/361", func(ctx context.Context, c Client) error {
			_, err := c.GetJobsByID(ctx, "361")
			return err
		}},
		{"diagnostics", "/slurm/v0.0.41/diag/", func(ctx context.Context, c Client) error {
			_, err := c.GetDiag(ctx)
			return err
		}},
	}
	responses := []struct {
		name        string
		status      int
		contentType string
		body        string
		wantError   string
	}{
		{"success", http.StatusOK, "application/json", `{"jobs":[],"statistics":{}}`, ""},
		{"HTTP error", http.StatusServiceUnavailable, "application/json", `{"errors":[{"description":"controller unavailable"}]}`, "status=503 errors=[controller unavailable]"},
		{"Slurm error", http.StatusOK, "application/json", `{"errors":[{"description":"access denied"}]}`, "errors=[access denied]"},
		{"malformed JSON", http.StatusOK, "application/json", `{`, "decode Slurm response"},
		{"unexpected content type", http.StatusOK, "text/html", `{"jobs":[],"statistics":{}}`, "unexpected content type"},
	}
	for _, operation := range operations {
		for _, response := range responses {
			t.Run(operation.name+"/"+response.name, func(t *testing.T) {
				body := &trackedResponseBody{Reader: strings.NewReader(response.body)}
				httpClient := &http.Client{Transport: responseRoundTripFunc(func(request *http.Request) (*http.Response, error) {
					assert.Equal(t, http.MethodGet, request.Method)
					assert.Equal(t, "/rest"+operation.path, request.URL.Path)
					assert.Equal(t, "test-token", request.Header.Get(headerSlurmUserToken))
					assert.Equal(t, headerApplicationJson, request.Header.Get(headerContentType))
					return &http.Response{
						StatusCode: response.status,
						Header:     http.Header{"Content-Type": []string{response.contentType}},
						Body:       body,
					}, nil
				})}
				c, err := NewClient("http://slurmrestd/rest/", jobOverflowTokenIssuer{}, httpClient)
				require.NoError(t, err)
				err = operation.call(t.Context(), c)
				if response.wantError == "" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, response.wantError)
				}
				assert.True(t, body.closed)
			})
		}
		t.Run(operation.name+"/canceled request", func(t *testing.T) {
			c, err := NewClient("http://slurmrestd", nil, &http.Client{})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			require.ErrorIs(t, operation.call(ctx, c), context.Canceled)
		})
	}
}
