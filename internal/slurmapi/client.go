package slurmapi

import (
	"context"
	"fmt"
	"net/http"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	slurmapispec "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/hashicorp/go-retryablehttp"
)

const (
	headerSlurmUserToken = "X-SLURM-USER-TOKEN"

	headerContentType     = "Content-Type"
	headerApplicationJson = "application/json"
)

func DefaultHTTPClient() *http.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil
	return retryClient.StandardClient()
}

type client struct {
	api.ClientWithResponsesInterface
}

type tokenIssuer interface {
	Issue(ctx context.Context) (string, error)
}

func NewClient(server string, tokenIssuer tokenIssuer, httpClient *http.Client) (*client, error) {
	if httpClient != nil {
		httpClient = DefaultHTTPClient()
	}

	headerFunc := func(ctx context.Context, req *http.Request) error {
		token, err := tokenIssuer.Issue(ctx)
		if err != nil {
			return fmt.Errorf("unable to issue jwt: %w", err)
		}

		req.Header.Add(headerSlurmUserToken, token)
		req.Header.Add(headerContentType, headerApplicationJson)
		return nil
	}

	c, err := api.NewClientWithResponses(
		server,
		api.WithHTTPClient(httpClient),
		api.WithRequestEditorFn(headerFunc),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create client: %v", err)
	}

	return &client{c}, nil
}

func (c *client) ListNodes(ctx context.Context) ([]Node, error) {
	getNodesResp, err := c.SlurmV0041GetNodesWithResponse(ctx, &slurmapispec.SlurmV0041GetNodesParams{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	if getNodesResp.JSON200 == nil {
		return nil, fmt.Errorf("json200 field is nil")
	}
	if getNodesResp.JSON200.Errors != nil && len(*getNodesResp.JSON200.Errors) != 0 {
		return nil, fmt.Errorf("list nodes responded with errors: %v", *getNodesResp.JSON200.Errors)
	}

	nodes := make([]Node, 0, len(getNodesResp.JSON200.Nodes))
	for _, n := range getNodesResp.JSON200.Nodes {
		node, err := NodeFromAPI(n)
		if err != nil {
			return nil, fmt.Errorf("convert node from api response: %w", err)
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}

func (c *client) GetNode(ctx context.Context, nodeName string) (Node, error) {
	getNodesResp, err := c.SlurmV0041GetNodeWithResponse(ctx, nodeName, &slurmapispec.SlurmV0041GetNodeParams{})
	if err != nil {
		return Node{}, fmt.Errorf("get node %s: %w", nodeName, err)
	}
	if getNodesResp.JSON200 == nil {
		return Node{}, fmt.Errorf("json200 field is nil, node name %s", nodeName)
	}
	if getNodesResp.JSON200.Errors != nil && len(*getNodesResp.JSON200.Errors) != 0 {
		return Node{}, fmt.Errorf("get node %s responded with errors: %v", nodeName, *getNodesResp.JSON200.Errors)
	}

	if nodeLength := len(getNodesResp.JSON200.Nodes); nodeLength != 1 {
		return Node{}, fmt.Errorf("expected only one node in response for get %s request, got %d", nodeName, nodeLength)
	}

	node, err := NodeFromAPI(getNodesResp.JSON200.Nodes[0])
	if err != nil {
		return Node{}, fmt.Errorf("convert node from api response: %w", err)
	}

	return node, nil
}
