package framework

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkerCleanupDoesNotEnableOwnership(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := WithWorkerCleanup(t.Context())
	selector := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator")
	workers, err := selector.PickWorkers(ctx, 1)
	require.NoError(t, err)
	require.Len(t, workers, 1)
	_, err = selector.Acquire(ctx, WorkerRequirements{Count: 1})
	require.ErrorContains(t, err, "ownership scope is required")
	_, err = NewSlurmClient(runtime).RunSlurm(ctx, "srun", SlurmCommandOptions{}, "hostname")
	require.NoError(t, err)
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
	assert.Empty(t, runtime.authority.leases)
	assert.NotContains(t, strings.Join(runtime.authority.calls, "\n"), "--reservation=")
}

func TestWorkerCleanupIsIsolatedAndSurvivesCancellation(t *testing.T) {
	first := WithWorkerCleanup(t.Context())
	second := WithWorkerCleanup(t.Context())
	first, cancel := context.WithCancel(first)
	cancel()
	failure := errors.New("restore scenario state")
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() { RecordWorkerCleanupError(context.WithoutCancel(first), failure) })
	}
	wg.Wait()
	require.ErrorIs(t, FinishWorkerReservation(first, nil), failure)
	require.ErrorIs(t, FinishWorkerReservation(first, nil), failure)
	require.NoError(t, FinishWorkerReservation(second, nil))
	require.NoError(t, FinishWorkerReservation(t.Context(), nil))
}
