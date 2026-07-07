package slurmapi

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

// sdkStub is a tiny v0041 SDK fake that intercepts only the two endpoints exercised by ListJobsWithParams.
// Any other SDK method panics via the nil embedded interface — that's deliberate, so unexpected calls fail loudly.
type sdkStub struct {
	api.ClientWithResponsesInterface

	controllerCalls int
	accountingCalls int

	lastControllerParams *api.SlurmV0041GetJobsParams
	lastAccountingParams *api.SlurmdbV0041GetJobsParams
}

func (s *sdkStub) SlurmV0041GetJobsWithResponse(_ context.Context, params *api.SlurmV0041GetJobsParams, _ ...api.RequestEditorFn) (*api.SlurmV0041GetJobsResponse, error) {
	s.controllerCalls++
	s.lastControllerParams = params
	return &api.SlurmV0041GetJobsResponse{JSON200: &api.V0041OpenapiJobInfoResp{Jobs: nil}}, nil
}

func (s *sdkStub) SlurmdbV0041GetJobsWithResponse(_ context.Context, params *api.SlurmdbV0041GetJobsParams, _ ...api.RequestEditorFn) (*api.SlurmdbV0041GetJobsResponse, error) {
	s.accountingCalls++
	s.lastAccountingParams = params
	return &api.SlurmdbV0041GetJobsResponse{JSON200: &api.V0041OpenapiSlurmdbdJobsResp{Jobs: nil}}, nil
}

func newTestClient(stub *sdkStub) *client {
	return &client{ClientWithResponsesInterface: stub}
}

func TestListJobsWithParams_ControllerSource(t *testing.T) {
	stub := &sdkStub{}
	c := newTestClient(stub)

	jobs, err := c.ListJobsWithParams(context.Background(), ListJobsParams{Source: JobSourceController})
	require.NoError(t, err)
	assert.Empty(t, jobs)

	assert.Equal(t, 1, stub.controllerCalls)
	assert.Equal(t, 0, stub.accountingCalls)
}

func TestListJobsWithParams_DefaultSourceIsController(t *testing.T) {
	stub := &sdkStub{}
	c := newTestClient(stub)

	_, err := c.ListJobsWithParams(context.Background(), ListJobsParams{})
	require.NoError(t, err)

	assert.Equal(t, 1, stub.controllerCalls)
	assert.Equal(t, 0, stub.accountingCalls)
}

func TestListJobsWithParams_AccountingSource_Window(t *testing.T) {
	tests := []struct {
		name        string
		lookback    time.Duration
		cluster     string
		wantCluster *string
	}{
		{name: "no cluster", lookback: time.Hour, cluster: "", wantCluster: nil},
		{name: "custom lookback", lookback: 30 * time.Minute, cluster: "", wantCluster: nil},
		{name: "with cluster filter", lookback: time.Hour, cluster: "soperator", wantCluster: ptr.To("soperator")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				stub := &sdkStub{}
				c := newTestClient(stub)

				now := time.Now()

				_, err := c.ListJobsWithParams(context.Background(), ListJobsParams{
					Source:             JobSourceAccounting,
					AccountingLookback: tt.lookback,
					AccountingCluster:  tt.cluster,
				})
				require.NoError(t, err)
				assert.Equal(t, 0, stub.controllerCalls)
				assert.Equal(t, 1, stub.accountingCalls)

				got := stub.lastAccountingParams
				require.NotNil(t, got)

				// State on the typed params is intentionally unset: the accounting query returns
				// jobs in any state during the window.
				assert.Nil(t, got.State)

				if tt.wantCluster == nil {
					assert.Nil(t, got.Cluster)
				} else {
					require.NotNil(t, got.Cluster)
					assert.Equal(t, *tt.wantCluster, *got.Cluster)
				}

				require.NotNil(t, got.StartTime)
				require.NotNil(t, got.EndTime)
				assert.Equal(t, fmt.Sprintf("uts%d", now.Add(-tt.lookback).Unix()), *got.StartTime)
				assert.Equal(t, fmt.Sprintf("uts%d", now.Add(accountingEndTimeSkew).Unix()), *got.EndTime)

				require.NotNil(t, got.SkipSteps)
				assert.Equal(t, "true", *got.SkipSteps)

				require.NotNil(t, got.DisableTruncateUsageTime)
				assert.Equal(t, "true", *got.DisableTruncateUsageTime)
			})
		})
	}
}

func TestListJobsWithParams_AccountingSource_RejectsNonPositiveLookback(t *testing.T) {
	stub := &sdkStub{}
	c := newTestClient(stub)

	_, err := c.ListJobsWithParams(context.Background(), ListJobsParams{
		Source:             JobSourceAccounting,
		AccountingLookback: 0,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AccountingLookback must be > 0")
	assert.Equal(t, 0, stub.accountingCalls)
}

func TestListJobsWithParams_UnsupportedSource(t *testing.T) {
	stub := &sdkStub{}
	c := newTestClient(stub)

	_, err := c.ListJobsWithParams(context.Background(), ListJobsParams{Source: JobSource("bogus")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unsupported source "bogus"`)
}
