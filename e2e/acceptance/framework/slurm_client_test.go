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

func TestSubmitBatchRunsAsUserWithCustomOutputDir(t *testing.T) {
	runtime := &submitBatchRuntime{}
	outputDir := "/opt/soperator-home/soperatorchecks/.acceptance/gpu profiling"

	job, err := NewSlurmClient(runtime).SubmitBatch(t.Context(), SbatchOptions{
		JobName:   "gpu-profile",
		Wrap:      "true",
		RunAsUser: "soperatorchecks",
		OutputDir: outputDir,
	})
	require.NoError(t, err)

	assert.Equal(t, outputDir+"/gpu-profile-123.out", job.StdoutPath)
	assert.Equal(t, outputDir+"/gpu-profile-123.err", job.StderrPath)
	assert.Contains(t, runtime.jailCommand,
		"sudo -iu 'soperatorchecks' -- mkdir -p "+ShellQuote(outputDir))
	assert.Contains(t, runtime.jailCommand,
		"sudo -iu 'soperatorchecks' -- sbatch")
	assert.Contains(t, runtime.jailCommand, "-o "+ShellQuote(outputDir+"/%x-%j.out"))
	assert.Contains(t, runtime.jailCommand, "-e "+ShellQuote(outputDir+"/%x-%j.err"))
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
