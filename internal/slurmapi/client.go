package slurmapi

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	api0043 "github.com/SlinkyProject/slurm-client/api/v0043"
	"github.com/hashicorp/go-retryablehttp"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// stalePendingMaxAge is the maximum submission age for a still-pending accounting record before
// the exporter treats it as a zombie and drops it. See isStaleAccountingPending for the rationale.
//
// 30 days is conservative on purpose: a job legitimately pending for that long is unusual but
// possible (long resource queues, held jobs that were never released). False positives at this
// threshold are accepted as a trade-off against the unbounded cardinality of forever-time_end=0
// rows. If a deployment has many genuinely long-pending jobs, this is the knob to revisit.
const stalePendingMaxAge = 30 * 24 * time.Hour

// withRepeatedStateFilter returns a request editor that appends one `state=X` query parameter
// per element of states. slurmrestd v0.0.41's data parser rejects a CSV value (treats `RUNNING,PENDING`
// as a single unknown flag name); the parser does accept multi-valued query parameters, so the
// fix is to send `state=RUNNING&state=PENDING` instead. Returns a no-op editor when states is empty.
func withRepeatedStateFilter(states []string) api.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		if len(states) == 0 {
			return nil
		}
		q := req.URL.Query()
		q.Del("state")
		for _, s := range states {
			q.Add("state", s)
		}
		req.URL.RawQuery = q.Encode()
		return nil
	}
}

// summarizeSlurmRESTBody extracts the descriptions/errors from a Slurm REST API JSON envelope,
// which all error responses follow ({"errors": [{"description": "...", "error": "..."}], ...}).
// Returns "errors=[...]" when the envelope parses, otherwise the raw body. Used to keep error
// logs scannable instead of dumping kilobytes of envelope metadata.
func summarizeSlurmRESTBody(body []byte) string {
	var parsed struct {
		Errors []struct {
			Description string `json:"description"`
			Error       string `json:"error"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && len(parsed.Errors) > 0 {
		msgs := make([]string, 0, len(parsed.Errors))
		for _, e := range parsed.Errors {
			switch {
			case e.Description != "":
				msgs = append(msgs, e.Description)
			case e.Error != "":
				msgs = append(msgs, e.Error)
			}
		}
		if len(msgs) > 0 {
			return fmt.Sprintf("errors=%v", msgs)
		}
	}
	return fmt.Sprintf("body=%s", string(body))
}

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
	// On retry exhaustion, surface the last response's status and a summary of the body so
	// callers can see why slurmrestd kept failing instead of just "giving up after N attempt(s)".
	retryClient.ErrorHandler = func(resp *http.Response, err error, numTries int) (*http.Response, error) {
		if resp != nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("giving up after %d attempt(s): status=%d %s", numTries, resp.StatusCode, summarizeSlurmRESTBody(body))
		}
		if err != nil {
			return nil, fmt.Errorf("giving up after %d attempt(s): %w", numTries, err)
		}
		return nil, fmt.Errorf("giving up after %d attempt(s)", numTries)
	}
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
	return c.ListJobsWithParams(ctx, ListJobsParams{Source: JobSourceController})
}

func (c *client) ListJobsWithParams(ctx context.Context, params ListJobsParams) ([]Job, error) {
	source := cmp.Or(params.Source, JobSourceController)

	switch source {
	case JobSourceController:
		return c.listControllerJobs(ctx)
	case JobSourceAccounting:
		return c.listAccountingJobs(ctx, params)
	default:
		return nil, fmt.Errorf("list jobs: unsupported source %q", source)
	}
}

func (c *client) listControllerJobs(ctx context.Context) ([]Job, error) {
	getJobsResp, err := c.SlurmV0041GetJobsWithResponse(ctx, &api.SlurmV0041GetJobsParams{})
	if err != nil {
		return nil, fmt.Errorf("list jobs from controller API: %w", err)
	}
	if getJobsResp.JSON200 == nil {
		return nil, fmt.Errorf("list jobs from controller API: status=%d %s", getJobsResp.StatusCode(), summarizeSlurmRESTBody(getJobsResp.Body))
	}
	if getJobsResp.JSON200.Errors != nil && len(*getJobsResp.JSON200.Errors) != 0 {
		return nil, fmt.Errorf("list jobs from controller API responded with errors: %v", *getJobsResp.JSON200.Errors)
	}

	jobs := make([]Job, 0, len(getJobsResp.JSON200.Jobs))
	for _, j := range getJobsResp.JSON200.Jobs {
		job, err := JobFromAPI(j)
		if err != nil {
			return nil, fmt.Errorf("convert job from controller api response: %w", err)
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

func (c *client) listAccountingJobs(ctx context.Context, params ListJobsParams) ([]Job, error) {
	if params.AccountingLookback <= 0 {
		return nil, fmt.Errorf("list jobs from accounting API: AccountingLookback must be > 0")
	}

	// Slurm's parse_time() rejects bare Unix epoch integers (despite the OpenAPI spec calling them
	// "UNIX timestamp") and is also picky about relative units (`sec` is too short for the 6-char
	// `xstrncasecmp(_, "second", 6)` check in 24.11). The "uts<epoch>" prefix is the explicit
	// Slurm-specific Unix-timestamp form that bypasses timezone handling and unit parsing.
	now := time.Now()
	startTime := fmt.Sprintf("uts%d", now.Add(-params.AccountingLookback).Unix())
	endTime := fmt.Sprintf("uts%d", now.Add(accountingEndTimeSkew).Unix())

	var clusterFilter *string
	if params.AccountingCluster != "" {
		clusterFilter = &params.AccountingCluster
	}
	getJobsResp, err := c.SlurmdbV0041GetJobsWithResponse(ctx, &api.SlurmdbV0041GetJobsParams{
		Cluster:   clusterFilter,
		StartTime: &startTime,
		EndTime:   &endTime,
		// State is intentionally nil: the SDK would serialize it as a single `state=A,B` CSV
		// value, but slurmrestd v0.0.41 rejects that as an unknown flag name. We add one
		// `state=X` query param per state via withRepeatedStateFilter below instead.
		State: nil,
		// Without this, slurmdbd clamps each job's reported start/end times to the query window
		// (matching `sacct --truncate`). Downstream metrics like slurm_job_duration_seconds rely
		// on the original timestamps, so any job whose lifetime extends outside the window would
		// otherwise expose synthetic timestamps and cap its duration at the lookback.
		DisableTruncateUsageTime: ptr.To("true"),
		// Per-step payloads are not used in downstream; skipping them keeps slurmdbd from
		// returning batch/extern/application step entries that would otherwise inflate every scrape on busy clusters.
		SkipSteps: ptr.To("true"),
	}, withRepeatedStateFilter(params.cleanedAccountingStates()))
	if err != nil {
		return nil, fmt.Errorf("list jobs from accounting API: %w", err)
	}
	if getJobsResp.JSON200 == nil {
		return nil, fmt.Errorf("list jobs from accounting API: status=%d %s", getJobsResp.StatusCode(), summarizeSlurmRESTBody(getJobsResp.Body))
	}
	if getJobsResp.JSON200.Errors != nil && len(*getJobsResp.JSON200.Errors) != 0 {
		return nil, fmt.Errorf("list jobs from accounting API responded with errors: %v", *getJobsResp.JSON200.Errors)
	}

	staleCutoff := now.Add(-stalePendingMaxAge).Unix()
	var droppedStale int

	jobs := make([]Job, 0, len(getJobsResp.JSON200.Jobs))
	for _, j := range getJobsResp.JSON200.Jobs {
		if isStaleAccountingPending(j, staleCutoff) {
			droppedStale++
			continue
		}
		job, err := JobFromAccountingAPI(j)
		if err != nil {
			return nil, fmt.Errorf("convert job from accounting api response: %w", err)
		}

		jobs = append(jobs, job)
	}

	if droppedStale > 0 {
		log.FromContext(ctx).Info("Dropped stale zombie-pending accounting jobs",
			"dropped", droppedStale,
			"max_age", stalePendingMaxAge.String(),
		)
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

func (c *client) GetReservation(ctx context.Context, name string) (Reservation, error) {
	resp, err := c.client0043.SlurmV0043GetReservationWithResponse(ctx, name, &api0043.SlurmV0043GetReservationParams{})
	if err != nil {
		return Reservation{}, fmt.Errorf("get reservation: %w", err)
	}
	if resp == nil {
		return Reservation{}, fmt.Errorf("get reservation response is nil")
	}
	if resp.JSON200 == nil {
		return Reservation{}, fmt.Errorf("json200 field is nil")
	}
	if resp.JSON200.Errors != nil && len(*resp.JSON200.Errors) != 0 {
		return Reservation{}, fmt.Errorf("get reservation responded with errors: %v", *resp.JSON200.Errors)
	}

	if reservationsLength := len(resp.JSON200.Reservations); reservationsLength != 1 {
		return Reservation{}, fmt.Errorf("expected only one reservation in response for get %s request, got %d", name, reservationsLength)
	}

	reservation := ReservationFromAPI(resp.JSON200.Reservations[0])

	return reservation, nil
}

func (c *client) PostMaintenanceReservation(ctx context.Context, name string, nodeList []string) error {
	resp, err := c.client0043.SlurmV0043PostReservationWithResponse(ctx, api0043.V0043ReservationDescMsg{
		Name:     ptr.To(name),
		NodeList: ptr.To(api0043.V0043HostlistString(nodeList)),
		Flags:    ptr.To([]api0043.V0043ReservationDescMsgFlags{api0043.V0043ReservationDescMsgFlagsMAINT, api0043.V0043ReservationDescMsgFlagsIGNOREJOBS}),
		Users:    ptr.To([]string{SlurmUserSoperatorchecks}),
		StartTime: &api0043.V0043Uint64NoValStruct{
			Number: ptr.To(time.Now().Unix()),
			Set:    ptr.To(true),
		},
		Duration: &api0043.V0043Uint32NoValStruct{
			Infinite: ptr.To(true),
		},
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
			Set:    ptr.To(true),
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
