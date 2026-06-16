package slurmproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type tokenIssuer interface {
	Issue(ctx context.Context) (string, error)
}

type Interface interface {
	RebootNodes(ctx context.Context, req RebootNodesRequest) error
}

type Client struct {
	server      string
	tokenIssuer tokenIssuer
	httpClient  *http.Client
}

func NewClient(server string, tokenIssuer tokenIssuer, httpClient *http.Client) (*Client, error) {
	if server == "" {
		return nil, fmt.Errorf("unable to create slurm controller proxy client: empty server URL")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		server:      strings.TrimRight(server, "/"),
		tokenIssuer: tokenIssuer,
		httpClient:  httpClient,
	}, nil
}

func (c *Client) RebootNodes(ctx context.Context, req RebootNodesRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal reboot nodes request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.server+EndpointRebootNodes, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create reboot nodes request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if c.tokenIssuer != nil {
		token, err := c.tokenIssuer.Issue(ctx)
		if err != nil {
			return fmt.Errorf("issue controller proxy token: %w", err)
		}
		if token != "" {
			httpReq.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("post reboot nodes request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("reboot nodes request failed: status=%d: %s", resp.StatusCode, responseError(resp.Body))
	}

	return nil
}

func responseError(body io.Reader) string {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Sprintf("read response body: %v", err)
	}
	var apiErr ErrorResponse
	if err := json.Unmarshal(data, &apiErr); err == nil && apiErr.Error != "" {
		return apiErr.Error
	}
	return string(data)
}
