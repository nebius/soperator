package main

import (
	"testing"

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
			name: "sources are trimmed normalized and deduplicated",
			flags: Flags{
				jobSources:        " controller, accounting,controller ,, ",
				accountingJobMode: " Completed ",
			},
			want: slurmapi.ListJobsParams{
				Sources: []slurmapi.JobSource{
					slurmapi.JobSourceController,
					slurmapi.JobSourceAccounting,
				},
				AccountingJobMode: slurmapi.AccountingJobModeCompleted,
			},
		},
		{
			name: "empty sources fall back to params normalization",
			flags: Flags{
				jobSources:        " , ",
				accountingJobMode: "all",
			},
			want: slurmapi.ListJobsParams{
				AccountingJobMode: slurmapi.AccountingJobModeAll,
			},
		},
		{
			name: "unsupported source",
			flags: Flags{
				jobSources:        "controller,invalid",
				accountingJobMode: "all",
			},
			wantErr: `unsupported job source "invalid"`,
		},
		{
			name: "unsupported accounting mode",
			flags: Flags{
				jobSources:        "controller",
				accountingJobMode: "forever",
			},
			wantErr: `unsupported accounting job mode "forever"`,
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
