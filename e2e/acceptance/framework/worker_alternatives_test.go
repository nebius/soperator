package framework

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedChoices(choices ...workerChoice) func(context.Context) ([]workerChoice, error) {
	return func(context.Context) ([]workerChoice, error) { return choices, nil }
}

func TestWorkerAlternativesPreferCPUWithoutHoldingPartialSets(t *testing.T) {
	for _, busyCPU := range []bool{false, true} {
		t.Run(fmt.Sprintf("busyCPU=%t", busyCPU), func(t *testing.T) {
			pool := NewWorkerPool()
			cpu, gpu := reservationWorkers("cpu0", "cpu1"), reservationWorkers("gpu0", "gpu1")
			holder := reservationForTest(pool, "holder")
			if busyCPU {
				_, err := holder.acquire(t.Context(), 1, fixedWorkers(cpu[:1]))
				require.NoError(t, err)
				defer holder.release(nil)
			}
			request := reservationForTest(pool, "rollout")
			workers, err := request.acquireAny(t.Context(), fixedChoices(
				workerChoice{count: 2, candidates: gpu},
				workerChoice{count: 2, candidates: cpu},
			))
			require.NoError(t, err)
			defer request.release(nil)
			if busyCPU {
				assert.Equal(t, []string{"gpu0", "gpu1"}, WorkerNames(workers))
				assert.Nil(t, pool.owners["cpu1"], "a blocked complete set must not retain its free member")
			} else {
				assert.Equal(t, []string{"cpu0", "cpu1"}, WorkerNames(workers))
			}
		})
	}
}

func TestWorkerAlternativesUseAnotherCPUSetBeforeGPU(t *testing.T) {
	pool := NewWorkerPool()
	choices := fixedChoices(
		workerChoice{count: 2, candidates: reservationWorkers("gpu0", "gpu1")},
		workerChoice{count: 2, candidates: reservationWorkers("cpu-a0", "cpu-a1")},
		workerChoice{count: 2, candidates: reservationWorkers("cpu-b0", "cpu-b1")},
	)
	first, second := reservationForTest(pool, "first"), reservationForTest(pool, "second")
	workers, err := first.acquireAny(t.Context(), choices)
	require.NoError(t, err)
	defer first.release(nil)
	assert.Equal(t, []string{"cpu-a0", "cpu-a1"}, WorkerNames(workers))
	workers, err = second.acquireAny(t.Context(), choices)
	require.NoError(t, err)
	defer second.release(nil)
	assert.Equal(t, []string{"cpu-b0", "cpu-b1"}, WorkerNames(workers))
	assert.Nil(t, pool.owners["gpu0"])
	assert.Nil(t, pool.owners["gpu1"])
}

func TestWorkerAlternativesWaitWithoutMixingSetsAndRetainQueueOrder(t *testing.T) {
	pool := NewWorkerPool()
	cpu, gpu := reservationWorkers("cpu0", "cpu1"), reservationWorkers("gpu0", "gpu1")
	holder := reservationForTest(pool, "holder")
	_, err := holder.acquire(t.Context(), 2, fixedWorkers([]WorkerInfo{cpu[0], gpu[0]}))
	require.NoError(t, err)
	defer holder.release(nil)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	older, younger := reservationForTest(pool, "older"), reservationForTest(pool, "younger")
	result := make(chan error, 1)
	go func() {
		_, err := older.acquireAny(ctx, fixedChoices(workerChoice{count: 2, candidates: cpu}, workerChoice{count: 2, candidates: gpu}))
		result <- err
	}()
	waitForRequests(t, pool, 1)
	pool.mu.Lock()
	assert.Nil(t, pool.owners["cpu1"])
	assert.Nil(t, pool.owners["gpu1"])
	pool.mu.Unlock()
	holder.release(nil)
	require.NoError(t, <-result)
	defer older.release(nil)
	assert.Equal(t, []string{"cpu0", "cpu1"}, WorkerNames(older.workers))
	workers, err := younger.acquireAny(ctx, fixedChoices(workerChoice{count: 2, candidates: cpu}, workerChoice{count: 2, candidates: gpu}))
	require.NoError(t, err)
	defer younger.release(nil)
	assert.Equal(t, []string{"gpu0", "gpu1"}, WorkerNames(workers))
}

func TestWorkerAlternativesQuarantineDoesNotHideHealthyFallback(t *testing.T) {
	pool := NewWorkerPool()
	cpu, gpu := reservationWorkers("cpu0", "cpu1"), reservationWorkers("gpu0", "gpu1")
	bad := reservationForTest(pool, "bad cleanup")
	_, err := bad.acquire(t.Context(), 2, fixedWorkers(cpu))
	require.NoError(t, err)
	bad.release(errors.New("worker did not recover"))
	fallback := reservationForTest(pool, "fallback")
	choices := fixedChoices(workerChoice{count: 2, candidates: cpu}, workerChoice{count: 2, candidates: gpu})
	workers, err := fallback.acquireAny(t.Context(), choices)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpu0", "gpu1"}, WorkerNames(workers))
	fallback.release(errors.New("GPU did not recover"))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err = reservationForTest(pool, "no fallback").acquireAny(ctx, choices)
	require.ErrorContains(t, err, "quarantined=")
	assert.NotErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, pool.waiting)
}

func TestWorkerAlternativesCancellationLeavesNoPartialAllocation(t *testing.T) {
	pool := NewWorkerPool()
	cpu, gpu := reservationWorkers("cpu0", "cpu1"), reservationWorkers("gpu0", "gpu1")
	holder := reservationForTest(pool, "holder")
	_, err := holder.acquire(t.Context(), 2, fixedWorkers([]WorkerInfo{cpu[0], gpu[0]}))
	require.NoError(t, err)
	defer holder.release(nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waiter := reservationForTest(pool, "cancelled rollout")
	result := make(chan error, 1)
	go func() {
		_, err := waiter.acquireAny(ctx, fixedChoices(workerChoice{count: 2, candidates: cpu}, workerChoice{count: 2, candidates: gpu}))
		result <- err
	}()
	waitForRequests(t, pool, 1)
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	assert.Empty(t, waiter.workers)
	pool.mu.Lock()
	assert.Empty(t, pool.waiting)
	assert.Len(t, pool.owners, 2)
	assert.Nil(t, pool.owners["cpu1"])
	assert.Nil(t, pool.owners["gpu1"])
	pool.mu.Unlock()
}

func TestGenericSnapshotSelectionPrefersCPUAndUsesMinimumGPUFallback(t *testing.T) {
	for _, count := range []int{1, 2, 3} {
		snapshot := WorkerSnapshot{
			NodeSets: []NodeSetInfo{{Name: "cpu", Replicas: 2}, {Name: "gpu", Replicas: 2, HasGPU: true}},
			Workers:  reservationWorkers("gpu0", "cpu0", "gpu1", "cpu1"),
		}
		workers, err := snapshot.PickWorkers(count)
		require.NoError(t, err)
		var gpuCount int
		for _, worker := range workers {
			if worker.HasGPU {
				gpuCount++
			}
		}
		assert.Equal(t, max(0, count-2), gpuCount)
	}
}
