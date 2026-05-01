package slurmapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestListJobsParamsAccountingStateFilter(t *testing.T) {
	tests := []struct {
		name   string
		params ListJobsParams
		want   *string
	}{
		{
			name:   "default mode",
			params: ListJobsParams{},
			want:   nil,
		},
		{
			name:   "pending",
			params: ListJobsParams{AccountingJobMode: AccountingJobModePending},
			want:   ptr.To("PENDING"),
		},
		{
			name:   "running",
			params: ListJobsParams{AccountingJobMode: AccountingJobModeRunning},
			want:   ptr.To("RUNNING"),
		},
		{
			name:   "completed",
			params: ListJobsParams{AccountingJobMode: AccountingJobModeCompleted},
			want:   ptr.To(completedAccountingJobStates),
		},
		{
			name:   "all",
			params: ListJobsParams{AccountingJobMode: AccountingJobModeAll},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.params.normalized().accountingStateFilter())
		})
	}
}

func TestDeduplicateJobs(t *testing.T) {
	assert.Equal(t, []Job{
		{ID: 10, Name: "accounting"},
		{ID: 11, Name: "other"},
	}, deduplicateJobs(
		[]Job{
			{ID: 10, Name: "accounting"},
			{ID: 11, Name: "other"},
		},
		[]Job{
			{ID: 10, Name: "controller"},
		},
	))
}

func TestDeduplicateJobs_FirstGroupWins(t *testing.T) {
	assert.Equal(t, []Job{
		{ID: 10, Name: "controller"},
		{ID: 12, Name: "controller-only"},
		{ID: 11, Name: "accounting-only"},
	}, deduplicateJobs(
		[]Job{
			{ID: 10, Name: "controller"},
			{ID: 12, Name: "controller-only"},
		},
		[]Job{
			{ID: 10, Name: "accounting"},
			{ID: 11, Name: "accounting-only"},
		},
	))
}
