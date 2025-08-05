package slurmapi

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	api0043 "github.com/SlinkyProject/slurm-client/api/v0043"
	"github.com/hashicorp/go-retryablehttp"
	"k8s.io/utils/ptr"
)

const (
	headerSlurmUserToken = "X-SLURM-USER-TOKEN"

	headerContentType     = "Content-Type"
	headerApplicationJson = "application/json"

	SlurmUserSoperatorchecks = "soperatorchecks"
)

func DefaultHTTPClient() *http.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil
	retryClient.RetryMax = 3
	retryClient.RetryWaitMin = 250 * time.Millisecond
	retryClient.RetryWaitMax = 2 * time.Second
	return retryClient.StandardClient()
}

type client struct {
	/**
	 * Refactor: hide the APIs of a specific SLURM REST API version
	 * Create methods like PostNode() in which we can decide which version to use.
	 */
	api.ClientWithResponsesInterface

	client0043 api0043.ClientWithResponsesInterface

	tokenIssuer tokenIssuer
}

type tokenIssuer interface {
	Issue(ctx context.Context) (string, error)
}

func NewClient(server string, tokenIssuer tokenIssuer, httpClient *http.Client) (Client, error) {
	if server == "" {
		return nil, fmt.Errorf("unable to create client: empty server URL")
	}
	if httpClient == nil {
		httpClient = DefaultHTTPClient()
	}

	apiClient := &client{
		tokenIssuer: normalizeIssuer(tokenIssuer),
	}

	c, err := api.NewClientWithResponses(
		server,
		api.WithHTTPClient(httpClient),
		api.WithRequestEditorFn(apiClient.setHeaders),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create client: %v", err)
	}

	apiClient.ClientWithResponsesInterface = c

	c0043, err := api0043.NewClientWithResponses(
		server,
		api0043.WithHTTPClient(httpClient),
		api0043.WithRequestEditorFn(apiClient.setHeaders),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create client: %v", err)
	}

	apiClient.client0043 = c0043

	return apiClient, nil
}

// normalizeIssuer converts a typed-nil inside an interface to a real nil.
func normalizeIssuer(t tokenIssuer) tokenIssuer {
	if t == nil {
		return nil
	}
	v := reflect.ValueOf(t)
	// handle pointers or interfaces wrapping a nil pointer
	if (v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface) && v.IsNil() {
		return nil
	}
	return t
}

func (c *client) setHeaders(ctx context.Context, req *http.Request) error {
	req.Header.Set(headerContentType, headerApplicationJson)

	ti := normalizeIssuer(c.tokenIssuer)
	if ti == nil {
		return nil
	}

	token, err := ti.Issue(ctx)
	if err != nil {
		return fmt.Errorf("unable to issue jwt: %w", err)
	}
	if token == "" {
		return nil
	}

	req.Header.Set(headerSlurmUserToken, token)
	return nil
}

func (c *client) ListNodes(ctx context.Context) ([]Node, error) {
	getNodesResp, err := c.SlurmV0041GetNodesWithResponse(ctx, &api.SlurmV0041GetNodesParams{})
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
	getNodesResp, err := c.SlurmV0041GetNodeWithResponse(ctx, nodeName, &api.SlurmV0041GetNodeParams{})
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

func (c *client) GetJobsByID(ctx context.Context, jobID string) ([]Job, error) {
	getJobResp, err := c.SlurmV0041GetJobWithResponse(ctx, jobID, &api.SlurmV0041GetJobParams{})
	if err != nil {
		return nil, fmt.Errorf("get job %s: %w", jobID, err)
	}
	if getJobResp.JSON200 == nil {
		return nil, fmt.Errorf("json200 field is nil for job ID %s", jobID)
	}
	if getJobResp.JSON200.Errors != nil && len(*getJobResp.JSON200.Errors) != 0 {
		return nil, fmt.Errorf("get job %s responded with errors: %v", jobID, *getJobResp.JSON200.Errors)
	}

	jobs := make([]Job, 0, len(getJobResp.JSON200.Jobs))
	for _, j := range getJobResp.JSON200.Jobs {
		job, err := JobFromAPI(j)
		if err != nil {
			return nil, fmt.Errorf("convert job from api response: %w", err)
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

func (c *client) ListJobs(ctx context.Context) ([]Job, error) {
	getJobsResp, err := c.SlurmV0041GetJobsWithResponse(ctx, &api.SlurmV0041GetJobsParams{})
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	if getJobsResp.JSON200 == nil {
		return nil, fmt.Errorf("json200 field is nil")
	}
	if getJobsResp.JSON200.Errors != nil && len(*getJobsResp.JSON200.Errors) != 0 {
		return nil, fmt.Errorf("list jobs responded with errors: %v", *getJobsResp.JSON200.Errors)
	}

	jobs := make([]Job, 0, len(getJobsResp.JSON200.Jobs))
	for _, j := range getJobsResp.JSON200.Jobs {
		job, err := JobFromAPI(j)
		if err != nil {
			return nil, fmt.Errorf("convert job from api response: %w", err)
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

func (c *client) GetDiag(ctx context.Context) (*api.V0041OpenapiDiagResp, error) {
	getDiagResp, err := c.SlurmV0041GetDiagWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("get diag: %w", err)
	}
	if getDiagResp.JSON200 == nil {
		return nil, fmt.Errorf("json200 field is nil")
	}
	if getDiagResp.JSON200.Errors != nil && len(*getDiagResp.JSON200.Errors) != 0 {
		return nil, fmt.Errorf("get diag responded with errors: %v", *getDiagResp.JSON200.Errors)
	}

	return getDiagResp.JSON200, nil
}

func (c *client) PostMaintenanceReservation(ctx context.Context, name string, nodeList []string) error {
	resp, err := c.client0043.SlurmV0043PostReservationWithResponse(ctx, api0043.V0043ReservationDescMsg{
		Name:     ptr.To(name),
		NodeList: ptr.To(api0043.V0043HostlistString(nodeList)),
		Flags:    ptr.To([]api0043.V0043ReservationDescMsgFlags{api0043.V0043ReservationDescMsgFlagsMAINT}),
		Users:    ptr.To([]string{SlurmUserSoperatorchecks}),
	})
	if err != nil {
		return fmt.Errorf("post reservation: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("post reservation response is nil")
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("post reservation returned status code %d body:%s", resp.StatusCode(), string(resp.Body))
	}
	if resp.JSON200.Errors != nil && len(*resp.JSON200.Errors) != 0 {
		return fmt.Errorf("post reservation returned errors: %v", *resp.JSON200.Errors)
	}
	return nil
}

func (c *client) StopReservation(ctx context.Context, name string) error {
	resp, err := c.client0043.SlurmV0043PostReservationWithResponse(ctx, api0043.V0043ReservationDescMsg{
		Name: ptr.To(name),
		Duration: &api0043.V0043Uint32NoValStruct{
			Number: ptr.To(int32(0)),
		},
	})
	if err != nil {
		return fmt.Errorf("stop reservation: %w", err)
	}
	if resp.JSON200.Errors != nil && len(*resp.JSON200.Errors) != 0 {
		return fmt.Errorf("stop reservation returned errors: %v", *resp.JSON200.Errors)
	}
	return nil
}
