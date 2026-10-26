package framework_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestWaitForJobSucceededWaitsForTerminalAccounting(t *testing.T) {
	for _, tt := range []struct {
		name    string
		records []string
	}{
		{name: "already completed", records: []string{"123|COMPLETED|0:0"}},
		{name: "missing record", records: []string{"", "123|COMPLETED|0:0"}},
		{name: "stale running record", records: []string{"123|RUNNING|0:0", "123|COMPLETED|0:0"}},
		{name: "accounting recovery", records: []string{"", "123|RUNNING|0:0", "123|COMPLETED|0:0"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				runtime := &jobWaitRuntime{records: tt.records}
				start := time.Now()

				err := framework.WaitForJobSucceeded(t.Context(), runtime, framework.NewSlurmClient(runtime), completionTestJob(), 10*time.Minute)

				require.NoError(t, err)
				assert.Equal(t, len(tt.records), runtime.accountingReads)
				assert.Equal(t, time.Duration(len(tt.records)-1)*framework.DefaultPollInterval, time.Since(start))
				assert.Equal(t, 10*time.Minute, runtime.executionTimeout)
			})
		})
	}
}

func TestWaitForJobSucceededRejectsTerminalFailures(t *testing.T) {
	for _, record := range []string{"123|FAILED|1:0", "123|CANCELLED|0:15", "123|TIMEOUT|0:15", "123|COMPLETED|1:0"} {
		t.Run(record, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				runtime := &jobWaitRuntime{records: []string{record}}
				start := time.Now()

				err := framework.WaitForJobSucceeded(t.Context(), runtime, framework.NewSlurmClient(runtime), completionTestJob(), time.Minute)

				require.ErrorContains(t, err, "job 123 finished with state=")
				assert.ErrorContains(t, err, "sacct: "+record)
				assert.ErrorContains(t, err, "stdout /tmp/job.out:\nworker-1")
				assert.ErrorContains(t, err, "stderr /tmp/job.err:\njob stderr")
				assert.Zero(t, time.Since(start))
			})
		})
	}
}

func TestWaitForJobSucceededBoundsAccountingWait(t *testing.T) {
	for _, tt := range []struct {
		name   string
		record string
	}{
		{name: "missing accounting"},
		{name: "stale accounting", record: "123|RUNNING|0:0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				runtime := &jobWaitRuntime{records: []string{tt.record}}
				start := time.Now()

				err := framework.WaitForJobSucceeded(t.Context(), runtime, framework.NewSlurmClient(runtime), completionTestJob(), 10*time.Minute)

				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.ErrorContains(t, err, "wait for job 123 completed successfully")
				assert.Equal(t, 2*time.Minute, time.Since(start))
			})
		})
	}
}

func TestWaitForJobSucceededRespectsCallerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		runtime := &jobWaitRuntime{}
		start := time.Now()

		err := framework.WaitForJobSucceeded(ctx, runtime, framework.NewSlurmClient(runtime), completionTestJob(), 10*time.Minute)

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, 15*time.Second, time.Since(start))
	})
}

func TestWaitForJobSucceededRespectsCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		time.AfterFunc(15*time.Second, cancel)
		runtime := &jobWaitRuntime{}
		start := time.Now()

		err := framework.WaitForJobSucceeded(ctx, runtime, framework.NewSlurmClient(runtime), completionTestJob(), 10*time.Minute)

		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 15*time.Second, time.Since(start))
	})
}

func TestWaitForJobSucceededPreservesQueueWaitFailure(t *testing.T) {
	waitErr := errors.New("queue wait timed out")
	runtime := &jobWaitRuntime{waitErr: waitErr, records: []string{"123|RUNNING|0:0"}}

	err := framework.WaitForJobSucceeded(t.Context(), runtime, framework.NewSlurmClient(runtime), completionTestJob(), time.Minute)

	require.ErrorIs(t, err, waitErr)
	assert.ErrorContains(t, err, "sacct: 123|RUNNING|0:0")
	assert.ErrorContains(t, err, "stdout /tmp/job.out:\nworker-1")
}

func completionTestJob() framework.SbatchJob {
	return framework.SbatchJob{ID: "123", StdoutPath: "/tmp/job.out", StderrPath: "/tmp/job.err"}
}

type jobWaitRuntime struct {
	framework.Runtime
	records          []string
	accountingReads  int
	executionTimeout time.Duration
	waitErr          error
}

func (r *jobWaitRuntime) WaitFor(ctx context.Context, _ string, timeout, _ time.Duration, condition func(context.Context) (bool, error)) error {
	r.executionTimeout = timeout
	if r.waitErr != nil {
		return r.waitErr
	}
	done, err := condition(ctx)
	if err != nil {
		return err
	}
	if !done {
		return errors.New("job is still queued")
	}
	return nil
}

func (r *jobWaitRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		switch {
		case strings.HasPrefix(command, "squeue "):
			return "", nil
		case strings.HasPrefix(command, "sacct "):
			r.accountingReads++
			if len(r.records) == 0 {
				return "", nil
			}
			record := r.records[0]
			if len(r.records) > 1 {
				r.records = r.records[1:]
			}
			return record, nil
		case strings.HasPrefix(command, "scontrol -o show job "):
			return "Restarts=0", nil
		case strings.HasPrefix(command, "tail -n 200 '/tmp/job.out'"):
			return "worker-1", nil
		case strings.HasPrefix(command, "tail -n 200 '/tmp/job.err'"):
			return "job stderr", nil
		default:
			return "", fmt.Errorf("unexpected jail command: %s", command)
		}
	})
}
