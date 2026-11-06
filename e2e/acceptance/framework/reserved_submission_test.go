package framework

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reservedSubmissionResult struct {
	submitBatchResult
	accepted bool
}

type reservedSubmissionRuntime struct {
	*reservationRuntime
	results    []reservedSubmissionResult
	attempts   int
	afterStart func()
}

func (r *reservedSubmissionRuntime) Jail() CommandScope {
	delegate := r.reservationRuntime.Jail()
	return reservationTestCommandScope{NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if !strings.Contains(command, "sbatch ") {
			return delegate.Run(ctx, command)
		}
		result := r.results[r.attempts]
		r.attempts++
		if result.accepted {
			r.jobs["123"] = true
		}
		if r.afterStart != nil {
			r.afterStart()
		}
		return result.out, result.err
	})}
}

func TestReservedSubmissionTracksIDDespiteExecutionError(t *testing.T) {
	for _, out := range []string{"123\n", "123;cluster\n"} {
		t.Run(strings.TrimSpace(out), func(t *testing.T) {
			pool := NewWorkerPool()
			ctx := acquireForFinalization(t, pool)
			execErr := errors.New("kubectl exec: unexpected EOF")
			runtime := &reservedSubmissionRuntime{
				reservationRuntime: newReservationRuntime(),
				results:            []reservedSubmissionResult{{submitBatchResult: submitBatchResult{out: out, err: execErr}, accepted: true}},
			}
			runtime.jobs["999"] = true
			job, err := NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{
				JobName: "uncertain", Wrap: "true", Nodelist: []string{"worker-0"},
			})
			require.ErrorIs(t, err, execErr)
			assert.True(t, job.IsZero(), "preserve the failed submission result")
			assert.Equal(t, 1, runtime.attempts, "an uncertain submission must never be retried")
			require.NoError(t, FinishWorkerReservation(ctx, runtime))
			assert.Equal(t, []string{"123"}, runtime.cancelled)
			assert.False(t, runtime.jobs["123"])
			assert.True(t, runtime.jobs["999"])
			assert.Empty(t, pool.quarantine, "verified job cleanup permits reuse")
		})
	}
}

func TestReservedSubmissionQuarantinesUnknownOutcomeEvenWhenWorkersAreIdle(t *testing.T) {
	execErr := errors.New("kubectl exec: unexpected EOF")
	for _, tt := range []struct {
		name string
		out  string
		err  error
	}{
		{name: "lost response", err: execErr},
		{name: "truncated numeric response", out: "12", err: execErr},
		{name: "empty successful response"},
		{name: "invalid successful response", out: "not-a-job-id\n"},
		{name: "invalid response with exec error", out: "warning\n", err: execErr},
		{name: "zero job id", out: "0\n"},
		{name: "signed job id", out: "+123\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewWorkerPool()
			ctx := acquireForFinalization(t, pool)
			runtime := &reservedSubmissionRuntime{
				reservationRuntime: newReservationRuntime(),
				results:            []reservedSubmissionResult{{submitBatchResult: submitBatchResult{out: tt.out, err: tt.err}, accepted: true}},
			}
			_, err := NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{
				JobName: "unknown", Wrap: "true", Nodelist: []string{"worker-0"},
			})
			require.Error(t, err)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			}
			finishErr := FinishWorkerReservation(ctx, runtime)
			require.ErrorContains(t, finishErr, "job id is unknown")
			if tt.err != nil {
				require.ErrorIs(t, finishErr, tt.err)
			}
			assert.Equal(t, 1, runtime.attempts)
			assert.Contains(t, pool.quarantine, "worker-0")
			assert.Empty(t, runtime.cancelled, "an unknown or truncated id must not cancel unrelated work")
			assert.True(t, runtime.jobs["123"], "quarantine contains the uncertainty; it cannot cancel an unidentified job")
			assert.Empty(t, pool.owners)
		})
	}
}

func TestReservedSubmissionPreExecutionFailuresDoNotQuarantine(t *testing.T) {
	preExecErr := errors.New("kubectl exec: dial tcp [::1]:443: i/o timeout")
	for _, cancelledBackoff := range []bool{false, true} {
		t.Run(map[bool]string{false: "retry limit", true: "cancelled backoff"}[cancelledBackoff], func(t *testing.T) {
			pool := NewWorkerPool()
			ctx, cancel := context.WithCancel(acquireForFinalization(t, pool))
			defer cancel()
			runtime := &reservedSubmissionRuntime{reservationRuntime: newReservationRuntime()}
			for range submitBatchRetryAttempts {
				runtime.results = append(runtime.results, reservedSubmissionResult{submitBatchResult: submitBatchResult{err: preExecErr}})
			}
			client := NewSlurmClient(runtime)
			client.submitRetryDelay = 0
			if cancelledBackoff {
				runtime.afterStart = cancel
				client.submitRetryDelay = time.Hour
			}
			_, err := client.SubmitBatch(ctx, SbatchOptions{JobName: "not submitted", Wrap: "true", Nodelist: []string{"worker-0"}})
			if cancelledBackoff {
				require.ErrorIs(t, err, context.Canceled)
				assert.Equal(t, 1, runtime.attempts)
			} else {
				require.ErrorIs(t, err, preExecErr)
				assert.Equal(t, submitBatchRetryAttempts, runtime.attempts)
			}
			require.NoError(t, FinishWorkerReservation(ctx, runtime))
			assert.Empty(t, pool.quarantine)
			assert.Empty(t, runtime.jobs)
		})
	}
}

func TestReservedSubmissionRejectsPlacementOverridesBeforeExecution(t *testing.T) {
	for _, flags := range [][]string{
		{"--nodelist=worker-1"}, {"--nodelist", "worker-1"}, {"--nodelist worker-1"},
		{"--nodel=worker-1"}, {"--nodes=2"}, {"--nodes", "2"}, {"--node=2"},
		{"-w", "worker-1"}, {"-wworker-1"}, {"-N", "2"}, {"-N2"},
		{"-vN2"}, {"-vw", "worker-1"}, {"--clusters=other"}, {"-M", "other"},
		{":"}, {"--"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			pool := NewWorkerPool()
			ctx := acquireForFinalization(t, pool)
			runtime := newReservationRuntime()
			_, err := NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{
				JobName: "override", Wrap: "true", Nodelist: []string{"worker-0"}, ExtraFlags: flags,
			})
			require.Error(t, err)
			assert.Empty(t, runtime.commands, "reject before remote sbatch execution")
			require.NoError(t, FinishWorkerReservation(ctx, runtime))
			assert.Empty(t, pool.quarantine)
		})
	}
}

func TestReservedSubmissionQuotesExtraArgumentsAndAcceptsSafeOptions(t *testing.T) {
	pool := NewWorkerPool()
	ctx := acquireForFinalization(t, pool)
	runtime := newReservationRuntime()
	comment := "--comment=literal $(hostname) ' --nodelist=worker-1"
	_, err := NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{
		JobName: "safe options", Wrap: "true", Nodelist: []string{"worker-0"},
		ExtraFlags: []string{"--time=5:00", "--no-requeue", "-t", "5:00", comment},
	})
	require.NoError(t, err)
	assert.Contains(t, runtime.commands[0], "'--time=5:00' '--no-requeue' '-t' '5:00' "+ShellQuote(comment))
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
}

func TestReservedSubmissionRejectsDuplicateWorkersBeforeExecution(t *testing.T) {
	pool := NewWorkerPool()
	ctx := acquireForFinalization(t, pool)
	runtime := newReservationRuntime()
	_, err := NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{
		JobName: "duplicate", Wrap: "true", Nodelist: []string{"worker-0", "worker-0"},
	})
	require.ErrorContains(t, err, "duplicate worker")
	assert.Empty(t, runtime.commands)
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
	assert.Empty(t, pool.quarantine)
}

func TestUnscopedSubmissionRetainsVerbatimExtraFlags(t *testing.T) {
	runtime := &submitBatchRuntime{}
	_, err := NewSlurmClient(runtime).SubmitBatch(t.Context(), SbatchOptions{
		JobName: "legacy", Wrap: "true", ExtraFlags: []string{"--nodes=2 --nodelist=worker-1"},
	})
	require.NoError(t, err)
	assert.Contains(t, runtime.jailCommand, " --nodes=2 --nodelist=worker-1 --wrap=")
}
