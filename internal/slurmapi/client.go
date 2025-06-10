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

	tokenIssuer tokenIssuer
}

type tokenIssuer interface {
	Issue(ctx context.Context) (string, error)
}

func NewClient(server string, tokenIssuer tokenIssuer, httpClient *http.Client) (Client, error) {
	if httpClient != nil {
		httpClient = DefaultHTTPClient()
	}

	apiClient := &client{
		tokenIssuer: tokenIssuer,
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

	return apiClient, nil
}

func (c *client) setHeaders(ctx context.Context, req *http.Request) error {
	token, err := c.tokenIssuer.Issue(ctx)
	if err != nil {
		return fmt.Errorf("unable to issue jwt: %w", err)
	}

	req.Header.Add(headerSlurmUserToken, token)
	req.Header.Add(headerContentType, headerApplicationJson)
	return nil
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

func (c *client) GetJobStatus(ctx context.Context, jobID string) (JobStatus, error) {
	getJobResp, err := c.SlurmV0041GetJobWithResponse(ctx, jobID, &slurmapispec.SlurmV0041GetJobParams{})
	if err != nil {
		return JobStatus{}, fmt.Errorf("get job %s: %w", jobID, err)
	}
	if getJobResp.JSON200 == nil {
		return JobStatus{}, fmt.Errorf("json200 field is nil for job ID %s", jobID)
	}
	if getJobResp.JSON200.Errors != nil && len(*getJobResp.JSON200.Errors) != 0 {
		return JobStatus{}, fmt.Errorf("get job %s responded with errors: %v", jobID, *getJobResp.JSON200.Errors)
	}
	if len(getJobResp.JSON200.Jobs) != 1 {
		return JobStatus{}, fmt.Errorf("expected one job in response for job ID %s, got %d", jobID, len(getJobResp.JSON200.Jobs))
	}

	job := getJobResp.JSON200.Jobs[0]

	status := JobStatus{
		Id:          job.JobId,
		Name:        job.Name,
		StateReason: job.StateReason,
		SubmitTime:  convertToMetav1Time(job.SubmitTime),
		StartTime:   convertToMetav1Time(job.StartTime),
		EndTime:     convertToMetav1Time(job.EndTime),
	}

	if job.JobState != nil && len(*job.JobState) > 0 {
		status.State = string((*job.JobState)[0])
		status.IsTerminateState = isTerminalState((*job.JobState)[0])
	} else {
		status.State = "UNKNOWN"
		status.IsTerminateState = true
	}

	return status, nil
}

func (c *client) ListJobs(ctx context.Context) ([]Job, error) {
	getJobsResp, err := c.SlurmV0041GetJobsWithResponse(ctx, &slurmapispec.SlurmV0041GetJobsParams{})
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
