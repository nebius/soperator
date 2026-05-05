package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

func TestBuildJobListParams(t *testing.T) {
	tests := []struct {
		name    string
		flags   Flags
		want    slurmapi.ListJobsParams
		wantErr string
	}{
		{
			name: "controller default",
			flags: Flags{
				jobSource:              "controller",
				accountingJobsLookback: "1h",
			},
			want: slurmapi.ListJobsParams{
				Source: slurmapi.JobSourceController,
			},
		},
		{
			name: "empty source defaults to controller",
			flags: Flags{
				accountingJobsLookback: "1h",
			},
			want: slurmapi.ListJobsParams{
				Source: slurmapi.JobSourceController,
			},
		},
		{
			name: "controller mode tolerates malformed lookback",
			flags: Flags{
				jobSource:              "controller",
				accountingJobsLookback: "garbage-from-stale-env",
			},
			want: slurmapi.ListJobsParams{
				Source: slurmapi.JobSourceController,
			},
		},
		{
			name: "controller mode tolerates empty lookback",
			flags: Flags{
				jobSource: "controller",
			},
			want: slurmapi.ListJobsParams{
				Source: slurmapi.JobSourceController,
			},
		},
		{
			name: "accounting requires non-empty lookback",
			flags: Flags{
				jobSource:              "accounting",
				accountingJobsLookback: "",
			},
			wantErr: "--accounting-jobs-lookback is required when --job-source=accounting",
		},
		{
			name: "accounting with states and custom lookback",
			flags: Flags{
				jobSource:              " Accounting ",
				accountingJobStates:    " RUNNING, PENDING ,, ",
				accountingJobsLookback: "30m",
			},
			want: slurmapi.ListJobsParams{
				Source:              slurmapi.JobSourceAccounting,
				AccountingJobStates: []string{"RUNNING", "PENDING"},
				AccountingLookback:  30 * time.Minute,
			},
		},
		{
			name: "cluster name plumbed through",
			flags: Flags{
				jobSource:              "accounting",
				accountingJobsLookback: "1h",
				clusterName:            "soperator",
			},
			want: slurmapi.ListJobsParams{
				Source:             slurmapi.JobSourceAccounting,
				AccountingLookback: time.Hour,
				AccountingCluster:  "soperator",
			},
		},
		{
			name: "Prometheus-style 1d lookback",
			flags: Flags{
				jobSource:              "accounting",
				accountingJobsLookback: "1d",
			},
			want: slurmapi.ListJobsParams{
				Source:             slurmapi.JobSourceAccounting,
				AccountingLookback: 24 * time.Hour,
			},
		},
		{
			name: "Prometheus-style 1w lookback",
			flags: Flags{
				jobSource:              "accounting",
				accountingJobsLookback: "1w",
			},
			want: slurmapi.ListJobsParams{
				Source:             slurmapi.JobSourceAccounting,
				AccountingLookback: 7 * 24 * time.Hour,
			},
		},
		{
			name: "Go-style fractional lookback",
			flags: Flags{
				jobSource:              "accounting",
				accountingJobsLookback: "1.5h",
			},
			want: slurmapi.ListJobsParams{
				Source:             slurmapi.JobSourceAccounting,
				AccountingLookback: 90 * time.Minute,
			},
		},
		{
			name: "Go-style mixed-units lookback",
			flags: Flags{
				jobSource:              "accounting",
				accountingJobsLookback: "2h45m30.5s",
			},
			want: slurmapi.ListJobsParams{
				Source:             slurmapi.JobSourceAccounting,
				AccountingLookback: 2*time.Hour + 45*time.Minute + 30*time.Second + 500*time.Millisecond,
			},
		},
		{
			name: "accounting empty states means no filter",
			flags: Flags{
				jobSource:              "accounting",
				accountingJobStates:    "",
				accountingJobsLookback: "1h",
			},
			want: slurmapi.ListJobsParams{
				Source:             slurmapi.JobSourceAccounting,
				AccountingLookback: time.Hour,
			},
		},
		{
			name: "unsupported source",
			flags: Flags{
				jobSource:              "invalid",
				accountingJobsLookback: "1h",
			},
			wantErr: `unsupported job source "invalid"`,
		},
		{
			name: "accounting requires positive lookback",
			flags: Flags{
				jobSource:              "accounting",
				accountingJobsLookback: "0s",
			},
			wantErr: "--accounting-jobs-lookback must be > 0 when --job-source=accounting",
		},
		{
			name: "invalid lookback duration",
			flags: Flags{
				jobSource:              "accounting",
				accountingJobsLookback: "not-a-duration",
			},
			wantErr: `parse --accounting-jobs-lookback: not a valid duration string: "not-a-duration"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildJobListParams(tt.flags)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{name: "prom days", input: "1d", want: 24 * time.Hour},
		{name: "prom weeks", input: "2w", want: 14 * 24 * time.Hour},
		{name: "prom hours", input: "1h", want: time.Hour},
		{name: "go fractional second", input: "0.5s", want: 500 * time.Millisecond},
		{name: "go fractional hour", input: "1.5h", want: 90 * time.Minute},
		{name: "go mixed units with fraction", input: "2h45m30.5s", want: 2*time.Hour + 45*time.Minute + 30*time.Second + 500*time.Millisecond},
		{name: "overlap integer minute", input: "30m", want: 30 * time.Minute},
		{name: "neither parser accepts", input: "garbage", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDuration(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
