package framework

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testReservation(pool *WorkerPool, name string) *workerReservation {
	return workerReservationFromContext(WithWorkerReservation(context.Background(), pool, name))
}

func testCandidates(names ...string) []workerCandidate {
	var candidates []workerCandidate
	for _, name := range names {
		candidates = append(candidates, workerCandidate{worker: WorkerInfo{Name: name, SlurmNode: SlurmNodeInfo{Name: name, State: "IDLE"}}})
	}
	return candidates
}

func fixedCandidates(names ...string) func(context.Context) (workerDiscovery, error) {
	return func(context.Context) (workerDiscovery, error) {
		return workerDiscovery{candidates: testCandidates(names...), configured: len(names)}, nil
	}
}

func waitForWorkerRequests(t *testing.T, pool *WorkerPool, count int) {
	t.Helper()
	require.Eventually(t, func() bool {
		pool.mu.Lock()
		defer pool.mu.Unlock()
		if len(pool.waiting) != count {
			return false
		}
		for _, request := range pool.waiting {
			if len(request.candidates) == 0 {
				return false
			}
		}
		return true
	}, time.Second, time.Millisecond)
}

func releaseTestReservation(r *workerReservation) {
	r.pool.mu.Lock()
	defer r.pool.mu.Unlock()
	r.state = reservationFinished
	r.pool.releaseLocked(r, nil)
}

func TestWorkerPoolAcquiresCompleteSetsAndBackfills(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	pool := NewWorkerPool()
	discover := fixedCandidates("worker-0", "worker-1")
	holder := testReservation(pool, "holder")
	_, err := holder.acquire(ctx, 1, discover)
	require.NoError(t, err)

	gang := testReservation(pool, "gang")
	gangDone := make(chan error, 1)
	go func() { _, err := gang.acquire(ctx, 2, discover); gangDone <- err }()
	waitForWorkerRequests(t, pool, 1)
	spare := testReservation(pool, "spare")
	workers, err := spare.acquire(ctx, 1, discover)
	require.NoError(t, err)
	assert.Equal(t, []string{"worker-1"}, WorkerNames(workers))
	independent := testReservation(pool, "independent")
	_, err = independent.acquire(ctx, 1, fixedCandidates("other-0"))
	require.NoError(t, err)
	releaseTestReservation(spare)
	pool.mu.Lock()
	assert.Nil(t, pool.owners["worker-1"], "a pending complete-set request must own no partial set")
	pool.mu.Unlock()
	releaseTestReservation(holder)
	require.NoError(t, <-gangDone)
	assert.Len(t, gang.workers, 2)
	releaseTestReservation(gang)
	releaseTestReservation(independent)
}

func TestWorkerPoolGrantsOldestFittingRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	pool := NewWorkerPool()
	discover := fixedCandidates("worker-0", "worker-1")
	holder := testReservation(pool, "holder")
	_, err := holder.acquire(ctx, 2, discover)
	require.NoError(t, err)
	older, younger := testReservation(pool, "older"), testReservation(pool, "younger")
	oldDone, youngDone := make(chan error, 1), make(chan error, 1)
	go func() { _, err := older.acquire(ctx, 2, discover); oldDone <- err }()
	waitForWorkerRequests(t, pool, 1)
	go func() { _, err := younger.acquire(ctx, 1, discover); youngDone <- err }()
	waitForWorkerRequests(t, pool, 2)
	releaseTestReservation(holder)
	require.NoError(t, <-oldDone)
	pool.mu.Lock()
	assert.Same(t, older, pool.owners["worker-0"])
	assert.Same(t, older, pool.owners["worker-1"])
	pool.mu.Unlock()
	releaseTestReservation(older)
	require.NoError(t, <-youngDone)
	releaseTestReservation(younger)
}

func TestWorkerPoolRevalidatesGrantAndRetainsQueuePosition(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	pool := NewWorkerPool()
	holder := testReservation(pool, "holder")
	_, err := holder.acquire(ctx, 1, fixedCandidates("worker-0"))
	require.NoError(t, err)
	waiter := testReservation(pool, "waiter")
	observedDown := make(chan struct{})
	finishDiscovery := make(chan struct{})
	var healthy atomic.Bool
	var reads atomic.Int32
	done := make(chan error, 1)
	go func() {
		_, err := waiter.acquire(ctx, 1, func(ctx context.Context) (workerDiscovery, error) {
			candidates := testCandidates("worker-0")
			if reads.Add(1) > 1 && !healthy.Load() {
				candidates[0].worker.SlurmNode.State = "DOWN"
				if reads.Load() == 2 {
					close(observedDown)
					select {
					case <-finishDiscovery:
					case <-ctx.Done():
						return workerDiscovery{}, ctx.Err()
					}
				}
			}
			return workerDiscovery{candidates: candidates, configured: len(candidates)}, nil
		})
		done <- err
	}()
	waitForWorkerRequests(t, pool, 1)
	pool.mu.Lock()
	pool.notifyLocked()
	pool.mu.Unlock()
	select {
	case <-observedDown:
	case <-ctx.Done():
		t.Fatal("waiter did not refresh readiness")
	}
	releaseTestReservation(holder)
	close(finishDiscovery)
	require.Eventually(t, func() bool {
		pool.mu.Lock()
		defer pool.mu.Unlock()
		return len(pool.waiting) == 1 && len(pool.owners) == 0 && pool.waiting[0].candidates[0].worker.SlurmNode.State == "DOWN"
	}, time.Second, time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("returned a stale grant: %v", err)
	default:
	}
	healthy.Store(true)
	pool.mu.Lock()
	pool.notifyLocked()
	pool.mu.Unlock()
	require.NoError(t, <-done)
	releaseTestReservation(waiter)
}

func TestWorkerPoolCancellationRacingWithGrantDoesNotLeak(t *testing.T) {
	for range 30 {
		pool := NewWorkerPool()
		discover := fixedCandidates("worker-0")
		holder, waiter := testReservation(pool, "holder"), testReservation(pool, "waiter")
		_, err := holder.acquire(t.Context(), 1, discover)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, err := waiter.acquire(ctx, 1, discover); done <- err }()
		waitForWorkerRequests(t, pool, 1)
		go cancel()
		releaseTestReservation(holder)
		<-done
		releaseTestReservation(waiter)
		pool.mu.Lock()
		assert.Empty(t, pool.waiting)
		assert.Empty(t, pool.owners)
		pool.mu.Unlock()
	}
}

func TestWorkerPoolRejectsConcurrentAcquisitionAndUpgrades(t *testing.T) {
	pool := NewWorkerPool()
	r := testReservation(pool, "one scope")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := r.acquire(ctx, 1, func(context.Context) (workerDiscovery, error) {
			candidates := testCandidates("worker-0")
			candidates[0].worker.SlurmNode.State = "DOWN"
			return workerDiscovery{candidates: candidates, configured: len(candidates)}, nil
		})
		done <- err
	}()
	waitForWorkerRequests(t, pool, 1)
	_, err := r.acquire(t.Context(), 1, fixedCandidates("worker-1"))
	require.ErrorContains(t, err, "complete set once")
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	_, err = r.acquire(t.Context(), 1, fixedCandidates("worker-0"))
	require.NoError(t, err)
	_, err = r.acquire(t.Context(), 2, fixedCandidates("worker-0", "worker-1"))
	require.ErrorContains(t, err, "complete set once")
	releaseTestReservation(r)
}

func TestWorkerPoolTimeoutAndQuarantineDiagnostics(t *testing.T) {
	pool := NewWorkerPool()
	pool.quarantine["worker-0"] = errors.New("restore kubelet")
	r := testReservation(pool, "next")
	_, err := r.acquire(t.Context(), 1, fixedCandidates("worker-0"))
	require.ErrorContains(t, err, "quarantined=restore kubelet")
	assert.Empty(t, pool.owners)
	assert.Empty(t, pool.waiting)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err = r.acquire(ctx, 1, func(context.Context) (workerDiscovery, error) {
		candidates := testCandidates("worker-1")
		candidates[0].reason = "host is not Ready"
		return workerDiscovery{candidates: candidates, configured: len(candidates)}, nil
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorContains(t, err, "host is not Ready")
	assert.Empty(t, pool.owners)
	assert.Empty(t, pool.waiting)
}

func TestWorkerPoolWaitsForMissingConfiguredCapacityDespiteQuarantine(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	pool := NewWorkerPool()
	pool.quarantine["worker-0"] = errors.New("unsafe cleanup")
	r := testReservation(pool, "remaining capacity")
	var recovered atomic.Bool
	done := make(chan error, 1)
	go func() {
		_, err := r.acquire(ctx, 1, func(context.Context) (workerDiscovery, error) {
			candidates := testCandidates("worker-0")
			if recovered.Load() {
				candidates = append(candidates, testCandidates("worker-1")...)
			}
			return workerDiscovery{candidates: candidates, configured: 2}, nil
		})
		done <- err
	}()
	waitForWorkerRequests(t, pool, 1)
	recovered.Store(true)
	pool.mu.Lock()
	pool.notifyLocked()
	pool.mu.Unlock()
	require.NoError(t, <-done)
	assert.Equal(t, []string{"worker-1"}, WorkerNames(r.workers))
	releaseTestReservation(r)
}
