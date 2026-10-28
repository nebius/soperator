package framework

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestSubmitBatchUsesScenarioArtifactDir(t *testing.T) {
	runtime := &submitBatchRuntime{}
	ctx := context.WithValue(t.Context(), scenarioArtifactsCtxKey{}, ScenarioArtifactPaths{
		JailDir: "/opt/soperator-outputs/shared/acceptance/scenarios/suite/scenario",
	})

	job, err := NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{
		JobName: "smoke",
		Wrap:    "true",
	})
	require.NoError(t, err)

	assert.Equal(t, "/opt/soperator-outputs/shared/acceptance/scenarios/suite/scenario/smoke-123.out", job.StdoutPath)
	assert.Contains(t, runtime.jailCommand, "scenarios/suite/scenario")
}

func TestSubmitBatchFallsBackOutsideScenario(t *testing.T) {
	runtime := &submitBatchRuntime{}

	job, err := NewSlurmClient(runtime).SubmitBatch(t.Context(), SbatchOptions{
		JobName: "smoke",
		Wrap:    "true",
	})
	require.NoError(t, err)

	assert.Equal(t, AcceptanceJobOutputDir+"/smoke-123.out", job.StdoutPath)
}

func TestSubmitBatchRetriesPreExecutionConnectionErrors(t *testing.T) {
	testCases := []struct {
		name string
		err  string
	}{
		{
			name: "API dial timeout",
			err:  "kubectl exec login-0: exit status 1: Unable to connect to the server: dial tcp [::1]:443: i/o timeout",
		},
		{
			name: "no route to API",
			err:  "kubectl exec login-0: exit status 1: error: dial tcp [::1]:443: connect: no route to host",
		},
		{
			name: "API connection timeout",
			err:  "kubectl exec login-0: exit status 1: error: dial tcp [::1]:443: connect: connection timed out",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &submitBatchRuntime{results: []submitBatchResult{
				{err: errors.New(tt.err)},
				{out: "123"},
			}}
			client := NewSlurmClient(runtime)
			client.submitRetryDelay = 0

			job, err := client.SubmitBatch(t.Context(), SbatchOptions{
				JobName: "smoke",
				Wrap:    "true",
			})
			require.NoError(t, err)

			assert.Equal(t, "123", job.ID)
			assert.Equal(t, 2, runtime.jailCalls)
		})
	}
}

func TestSubmitBatchDoesNotRetryUncertainFailures(t *testing.T) {
	testCases := []struct {
		name string
		out  string
		err  error
	}{
		{
			name: "unexpected EOF",
			err:  errors.New("kubectl exec login-0: exit status 1: error: unexpected EOF"),
		},
		{
			name: "sbatch rejection",
			err:  errors.New("kubectl exec login-0: exit status 1: sbatch: error: Invalid account"),
		},
		{
			name: "partial stdout",
			out:  "123\n",
			err:  errors.New("kubectl exec login-0: exit status 1: error: dial tcp [::1]:443: i/o timeout"),
		},
		{
			name: "non-kubectl dial error",
			err:  errors.New("dial tcp [::1]:443: i/o timeout"),
		},
		{
			name: "context canceled",
			err:  context.Canceled,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &submitBatchRuntime{results: []submitBatchResult{
				{out: tt.out, err: tt.err},
				{out: "123"},
			}}
			client := NewSlurmClient(runtime)
			client.submitRetryDelay = 0

			_, err := client.SubmitBatch(t.Context(), SbatchOptions{
				JobName: "smoke",
				Wrap:    "true",
			})
			require.Error(t, err)

			assert.Equal(t, 1, runtime.jailCalls)
		})
	}
}

func TestSubmitBatchStopsAfterRetryLimit(t *testing.T) {
	retryableErr := errors.New("kubectl exec login-0: exit status 1: error: dial tcp [::1]:443: i/o timeout")
	runtime := &submitBatchRuntime{results: []submitBatchResult{
		{err: retryableErr},
		{err: retryableErr},
		{err: retryableErr},
		{out: "123"},
	}}
	client := NewSlurmClient(runtime)
	client.submitRetryDelay = 0

	_, err := client.SubmitBatch(t.Context(), SbatchOptions{
		JobName: "smoke",
		Wrap:    "true",
	})
	require.Error(t, err)

	assert.ErrorIs(t, err, retryableErr)
	assert.Equal(t, submitBatchRetryAttempts, runtime.jailCalls)
}

func TestSubmitBatchStopsWhenRetryBecomesUncertain(t *testing.T) {
	runtime := &submitBatchRuntime{results: []submitBatchResult{
		{err: errors.New("kubectl exec login-0: exit status 1: error: dial tcp [::1]:443: connect: no route to host")},
		{err: errors.New("kubectl exec login-0: exit status 1: error: unexpected EOF")},
		{out: "123"},
	}}
	client := NewSlurmClient(runtime)
	client.submitRetryDelay = 0

	_, err := client.SubmitBatch(t.Context(), SbatchOptions{
		JobName: "smoke",
		Wrap:    "true",
	})
	require.ErrorContains(t, err, "unexpected EOF")

	assert.Equal(t, 2, runtime.jailCalls)
}

func TestJobReleaseGateQuotesPathAndHasDeadline(t *testing.T) {
	path := "/tmp/a 'job'; $(touch /tmp/unsafe)"
	script := JobReleaseGate(path, time.Minute)

	assert.Equal(t, "timeout 60s sh -c "+ShellQuote("while [ ! -f "+ShellQuote(path)+" ]; do sleep 1; done"), script)
	assert.Contains(t, JobReleaseGate(path, 100*time.Millisecond), "timeout 0.1s sh -c ")
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
	jailCalls   int
	results     []submitBatchResult
}

type submitBatchResult struct {
	out string
	err error
}

func (r *submitBatchRuntime) Jail() CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		r.jailCommand = command
		resultIndex := r.jailCalls
		r.jailCalls++
		if resultIndex < len(r.results) {
			return r.results[resultIndex].out, r.results[resultIndex].err
		}
		return "123", nil
	})
}
