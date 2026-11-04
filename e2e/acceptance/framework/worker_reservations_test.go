package framework

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func reservationForTest(pool *WorkerPool, name string) *WorkerReservation {
	return workerReservation(WithWorkerReservation(context.Background(), pool, name))
}

func reservationWorkers(names ...string) []WorkerInfo {
	var workers []WorkerInfo
	for _, name := range names {
		workers = append(workers, WorkerInfo{Name: name, HasGPU: strings.HasPrefix(name, "gpu"), SlurmNode: SlurmNodeInfo{Name: name, State: "IDLE"}})
	}
	return workers
}

func fixedWorkers(workers []WorkerInfo) func(context.Context) ([]WorkerInfo, error) {
	return func(context.Context) ([]WorkerInfo, error) { return workers, nil }
}

func waitForRequests(t *testing.T, pool *WorkerPool, count int) {
	t.Helper()
	require.Eventually(t, func() bool {
		pool.mu.Lock()
		defer pool.mu.Unlock()
		return len(pool.waiting) == count
	}, time.Second, time.Millisecond)
}

func TestWorkerReservationsBackfillAndAcquireMultipleWorkersAtomically(t *testing.T) {
	pool := NewWorkerPool()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	gpu := fixedWorkers(reservationWorkers("gpu0", "gpu1"))
	replacement := reservationForTest(pool, "replacement")
	selected, err := replacement.acquire(ctx, 1, gpu)
	require.NoError(t, err)
	assert.Equal(t, "gpu0", selected[0].Name)
	gang := reservationForTest(pool, "two GPUs")
	gangResult := make(chan error, 1)
	go func() { _, err := gang.acquire(ctx, 2, gpu); gangResult <- err }()
	waitForRequests(t, pool, 1)
	spare := reservationForTest(pool, "spare GPU")
	selected, err = spare.acquire(ctx, 1, gpu)
	require.NoError(t, err)
	assert.Equal(t, "gpu1", selected[0].Name)
	cpu := reservationForTest(pool, "CPU")
	_, err = cpu.acquire(ctx, 1, fixedWorkers(reservationWorkers("cpu0")))
	require.NoError(t, err)
	spare.release(nil)
	pool.mu.Lock()
	assert.Nil(t, pool.owners["gpu1"], "a two-worker waiter must not hold a partial allocation")
	pool.mu.Unlock()
	replacement.release(nil)
	require.NoError(t, <-gangResult)
	assert.Len(t, gang.workers, 2)
	gang.release(nil)
	cpu.release(nil)
}

func TestWorkerReservationsPreferOldestFittingRequest(t *testing.T) {
	pool := NewWorkerPool()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	gpu := fixedWorkers(reservationWorkers("gpu0", "gpu1"))
	holder := reservationForTest(pool, "holder")
	_, err := holder.acquire(ctx, 2, gpu)
	require.NoError(t, err)
	older, younger := reservationForTest(pool, "older"), reservationForTest(pool, "younger")
	oldResult, youngResult := make(chan error, 1), make(chan error, 1)
	go func() { _, err := older.acquire(ctx, 2, gpu); oldResult <- err }()
	waitForRequests(t, pool, 1)
	go func() { _, err := younger.acquire(ctx, 1, gpu); youngResult <- err }()
	waitForRequests(t, pool, 2)
	holder.release(nil)
	require.NoError(t, <-oldResult)
	pool.mu.Lock()
	assert.Same(t, older, pool.owners["gpu0"])
	assert.Same(t, older, pool.owners["gpu1"])
	pool.mu.Unlock()
	older.release(nil)
	require.NoError(t, <-youngResult)
	younger.release(nil)
}

func TestWorkerReservationsRecoveryCancellationAndQuarantine(t *testing.T) {
	pool := NewWorkerPool()
	ctx, cancel := context.WithCancel(t.Context())
	r := reservationForTest(pool, "cancelled")
	result := make(chan error, 1)
	go func() {
		_, err := r.acquire(ctx, 1, fixedWorkers([]WorkerInfo{{Name: "gpu0", SlurmNode: SlurmNodeInfo{State: "DOWN"}}}))
		result <- err
	}()
	waitForRequests(t, pool, 1)
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	waitForRequests(t, pool, 0)
	pool.mu.Lock()
	assert.Empty(t, pool.owners)
	pool.mu.Unlock()

	var observationMu sync.Mutex
	healthy := false
	recovered := reservationForTest(pool, "recovered")
	go func() {
		_, err := recovered.acquire(t.Context(), 1, func(context.Context) ([]WorkerInfo, error) {
			observationMu.Lock()
			defer observationMu.Unlock()
			workers := reservationWorkers("gpu0")
			if !healthy {
				workers[0].SlurmNode.State = "IDLE+DRAIN"
			}
			return workers, nil
		})
		result <- err
	}()
	waitForRequests(t, pool, 1)
	observationMu.Lock()
	healthy = true
	observationMu.Unlock()
	pool.mu.Lock()
	pool.notify()
	pool.mu.Unlock()
	require.NoError(t, <-result)
	recovered.release(errors.New("cleanup did not recover the worker"))
	_, err := reservationForTest(pool, "next").acquire(t.Context(), 1, fixedWorkers(reservationWorkers("gpu0")))
	require.ErrorContains(t, err, "quarantined=cleanup did not recover")
}

func TestWorkerReservationsPreferCPUAndRejectIncrementalClaims(t *testing.T) {
	r := reservationForTest(NewWorkerPool(), "generic")
	workers := fixedWorkers(reservationWorkers("gpu0", "cpu0"))
	selected, err := r.acquire(t.Context(), 1, workers)
	require.NoError(t, err)
	assert.Equal(t, "cpu0", selected[0].Name)
	_, err = r.acquire(t.Context(), 2, workers)
	require.ErrorContains(t, err, "complete worker set together")
	r.release(nil)
}

func TestCleanupFailureQuarantinesReservationAndPreservesContext(t *testing.T) {
	pool := NewWorkerPool()
	ctx := WithWorkerReservation(t.Context(), pool, "cleanup failure")
	r := workerReservation(ctx)
	_, err := r.acquire(ctx, 1, fixedWorkers(reservationWorkers("gpu0")))
	require.NoError(t, err)
	cleanupErr := errors.New("stop unmanaged workload")
	RecordWorkerCleanupError(context.WithoutCancel(ctx), cleanupErr)
	require.ErrorIs(t, FinishWorkerReservation(ctx, nil), cleanupErr)
	assert.ErrorIs(t, pool.quarantine["gpu0"], cleanupErr)
	assert.Empty(t, pool.owners)
}

func TestReservedBatchJobsRequireWorkerAcquisition(t *testing.T) {
	runtime := &submitBatchRuntime{}
	ctx := WithWorkerReservation(t.Context(), NewWorkerPool(), "missing acquisition")
	_, err := NewSlurmClient(runtime).SubmitBatch(ctx, SbatchOptions{
		JobName: "unreserved", Wrap: "true", Nodelist: []string{"gpu0"},
	})
	require.ErrorContains(t, err, "reserve workers before submitting a job")
	assert.Empty(t, runtime.jailCommand, "reject the submission before it reaches Slurm")
}

func TestReservedBatchJobsCannotEscapeTheirWorkers(t *testing.T) {
	ctx := WithWorkerReservation(t.Context(), NewWorkerPool(), "pinned")
	r := workerReservation(ctx)
	_, err := r.acquire(ctx, 1, fixedWorkers(reservationWorkers("gpu0")))
	require.NoError(t, err)
	client := NewSlurmClient(nil)
	_, err = client.SubmitBatch(ctx, SbatchOptions{JobName: "unpinned", Wrap: "true"})
	require.ErrorContains(t, err, "explicit nodelist")
	_, err = client.SubmitBatch(ctx, SbatchOptions{JobName: "wrong worker", Wrap: "true", Nodelist: []string{"gpu1"}})
	require.ErrorContains(t, err, "outside the scenario reservation")
	r.release(nil)
}

func TestWorkerReservationCancellationRacingWithGrantDoesNotLeak(t *testing.T) {
	for range 20 {
		pool := NewWorkerPool()
		workers := fixedWorkers(reservationWorkers("gpu0"))
		holder, waiter := reservationForTest(pool, "holder"), reservationForTest(pool, "waiter")
		_, err := holder.acquire(t.Context(), 1, workers)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, err := waiter.acquire(ctx, 1, workers); done <- err }()
		waitForRequests(t, pool, 1)
		go cancel()
		holder.release(nil)
		<-done
		waiter.release(nil)
		pool.mu.Lock()
		assert.Empty(t, pool.owners)
		assert.Empty(t, pool.waiting)
		pool.mu.Unlock()
	}
}

type reservationHealthRuntime struct {
	Runtime
	ready      bool
	queueReads int
	cancelled  []string
	nodeStates []string
	nodeReads  int
	onNodeRead func()
}

func (r *reservationHealthRuntime) Controller() CommandScope {
	return NewCommandScope(func(ctx context.Context, _ string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		state := "IDLE"
		if len(r.nodeStates) != 0 {
			state = r.nodeStates[min(r.nodeReads, len(r.nodeStates)-1)]
		}
		r.nodeReads++
		if r.onNodeRead != nil {
			r.onNodeRead()
		}
		return "NodeName=gpu0 State=" + state, nil
	})
}

func (r *reservationHealthRuntime) Kubectl() ArgsScope {
	return NewArgsScope(func(ctx context.Context, _ ...string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		status := "False"
		if r.ready {
			status = "True"
		}
		return fmt.Sprintf(`{"items":[{"metadata":{"name":"gpu0","namespace":"soperator"},"spec":{"hostname":"gpu0","nodeName":"host0"},"status":{"conditions":[{"type":"Ready","status":%q}]}}]}`, status), nil
	})
}

func (r *reservationHealthRuntime) Jail() CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if strings.HasPrefix(command, "squeue") {
			r.queueReads++
			if r.queueReads == 1 {
				return "123\n999\n", nil
			}
			return "999\n", nil
		}
		r.cancelled = append(r.cancelled, command)
		return "", nil
	})
}

func (r *reservationHealthRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	var lastErr error
	for range 4 {
		done, err := condition(ctx)
		if done && err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

func (*reservationHealthRuntime) Logf(string, ...any) {}

func TestFinishReservationChecksRecoveryAndCancelsOnlyOwnedJobs(t *testing.T) {
	for _, ready := range []bool{false, true} {
		pool := NewWorkerPool()
		ctx := WithWorkerReservation(t.Context(), pool, "finalize")
		r := workerReservation(ctx)
		_, err := r.acquire(ctx, 1, fixedWorkers(reservationWorkers("gpu0")))
		require.NoError(t, err)
		r.jobs["123"] = struct{}{}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		runtime := &reservationHealthRuntime{ready: ready}
		err = FinishWorkerReservation(cancelled, runtime)
		assert.Equal(t, []string{"scancel '123'"}, runtime.cancelled)
		assert.Empty(t, pool.owners)
		if ready {
			require.NoError(t, err)
			assert.Empty(t, pool.quarantine)
		} else {
			require.ErrorContains(t, err, "not Ready")
			assert.Contains(t, pool.quarantine, "gpu0")
		}
	}
}

func TestFinishReservationWaitsForUntrackedJobEpilog(t *testing.T) {
	pool := NewWorkerPool()
	ctx := WithWorkerReservation(t.Context(), pool, "external utility")
	reservation := workerReservation(ctx)
	_, err := reservation.acquire(ctx, 1, fixedWorkers(reservationWorkers("gpu0")))
	require.NoError(t, err)

	runtime := &reservationHealthRuntime{
		ready:      true,
		nodeStates: []string{"IDLE+CLOUD+COMPLETING", "IDLE+CLOUD"},
		onNodeRead: func() {
			assert.Same(t, reservation, pool.owners["gpu0"], "retain ownership until the utility's epilog finishes")
		},
	}
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
	assert.Equal(t, 2, runtime.nodeReads)
	assert.Empty(t, runtime.cancelled)
	assert.Empty(t, pool.owners)
	assert.Empty(t, pool.quarantine)
}
