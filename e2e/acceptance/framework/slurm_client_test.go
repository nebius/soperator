package framework

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubmitBatchQuotesJobName(t *testing.T) {
	runtime := &submitBatchRuntime{}
	jobName := `job name ' $(echo unsafe)`

	job, err := NewSlurmClient(runtime).SubmitBatch(t.Context(), SbatchOptions{
		JobName: jobName,
		Wrap:    "true",
	})
	require.NoError(t, err)

	assert.Equal(t, jobName, job.JobName)
	assert.Contains(t, runtime.jailCommand, "--job-name="+ShellQuote(jobName))
}

func TestParseSacctJob(t *testing.T) {
	dump := `
123.batch|COMPLETED|0:0||
123|FAILED|1:0|Prolog failure|
`

	state, exitCode, reason, found := parseSacctJob(dump, "123")

	assert.True(t, found)
	assert.Equal(t, "FAILED", state)
	assert.Equal(t, "1:0", exitCode)
	assert.Equal(t, "Prolog failure", reason)
}

func TestSlurmJobInfoCompletedSuccessfully(t *testing.T) {
	info := SlurmJobInfo{
		SacctFound: true,
		SacctState: "COMPLETED",
		SacctExit:  "0:0",
	}

	assert.True(t, info.CompletedSuccessfully())
	assert.False(t, info.IsAlive())
}

type submitBatchRuntime struct {
	Runtime
	jailCommand string
}

func (r *submitBatchRuntime) Jail() CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		r.jailCommand = command
		return "123", nil
	})
}
