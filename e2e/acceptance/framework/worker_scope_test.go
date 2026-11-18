package framework

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalizationWaitsForLongEpilog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runtime := newReservationRuntime()
		ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
		id, _ := WorkerReservationName(ctx)
		runtime.authority.jobs[id] = []string{"lost-id"}
		runtime.authority.keepJobs = true
		runtime.authority.states["worker-0"] = "IDLE+COMPLETING"
		go func() {
			time.Sleep(7 * time.Minute)
			a := runtime.authority
			a.mu.Lock()
			defer a.mu.Unlock()
			delete(a.jobs, id)
			a.states["worker-0"] = "IDLE"
		}()
		start := time.Now()
		require.NoError(t, FinishWorkerReservation(ctx, runtime))
		assert.GreaterOrEqual(t, time.Since(start), 7*time.Minute)
		assert.Equal(t, 1, runtime.authority.cancelled)
		assert.Empty(t, runtime.authority.leases)
	})
}

func TestUnsafeRestorationRetainsNativeLeaseAcrossRunners(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	restorationErr := errors.New("restore fault marker")
	RecordWorkerCleanupError(ctx, restorationErr)
	require.ErrorIs(t, FinishWorkerReservation(ctx, runtime), restorationErr)
	require.ErrorIs(t, FinishWorkerReservation(ctx, runtime), restorationErr)
	require.Len(t, runtime.authority.leases, 1)
	contender, cancel := context.WithTimeout(WithWorkerReservation(t.Context(), NewReservationManager(), "another runner"), 10*time.Millisecond)
	defer cancel()
	_, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(contender, WorkerRequirements{Count: 1, Names: []string{"worker-0"}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRecoveryAdmissionClosesOnlyAtFinalization(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	require.NoError(t, RunTargetedWorkerSmoke(ctx, runtime, WorkerInfo{Name: "worker-0"}))
	r := workerReservationFromContext(ctx)
	done, err := r.beginSubmission()
	require.NoError(t, err)
	finished := make(chan error, 1)
	go func() { finished <- FinishWorkerReservation(ctx, runtime) }()
	require.Eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.state == reservationFinalizing }, time.Second, time.Millisecond)
	_, err = r.beginSubmission()
	require.ErrorContains(t, err, "closing")
	select {
	case <-finished:
		t.Fatal("released before an admitted operation returned")
	default:
	}
	done()
	require.NoError(t, <-finished)
	require.Error(t, RunTargetedWorkerSmoke(ctx, runtime, WorkerInfo{Name: "worker-0"}))
}

func TestRecoveryChecksHealthOccupancyAndPower(t *testing.T) {
	for _, test := range []struct {
		name, state       string
		foreign           string
		pod, host, active bool
		wantErr           bool
	}{
		{name: "allocated with no allocation", state: "ALLOCATED", pod: true, host: true},
		{name: "mixed with no allocation", state: "MIXED", pod: true, host: true},
		{name: "foreign occupancy", state: "ALLOCATED", foreign: "foreign-job", pod: true, host: true, wantErr: true},
		{name: "bad host", state: "IDLE", pod: true, wantErr: true},
		{name: "bad pod", state: "IDLE", host: true, wantErr: true},
		{name: "powered down baseline", state: "IDLE+POWERED_DOWN"},
		{name: "require active", state: "IDLE+POWERED_DOWN", active: true, wantErr: true},
		{name: "drained", state: "IDLE+DRAIN", pod: true, host: true, wantErr: true},
		{name: "epilog", state: "IDLE+COMPLETING", pod: true, host: true, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := newReservationRuntime()
			options := ReservationOptions{RecoveryTimeout: 5 * time.Millisecond, RequireActiveRecovery: test.active}
			base := WithReservationOptions(t.Context(), options)
			ctx := WithWorkerReservation(base, NewReservationManager(), test.name)
			_, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(ctx, WorkerRequirements{Count: 1, Names: []string{"worker-0"}})
			require.NoError(t, err)
			runtime.authority.states["worker-0"] = test.state
			runtime.authority.podReady = test.pod
			runtime.authority.hostReady = test.host
			runtime.authority.foreign = test.foreign
			err = FinishWorkerReservation(ctx, runtime)
			if test.wantErr {
				require.Error(t, err)
				require.Len(t, runtime.authority.leases, 1)
			} else {
				require.NoError(t, err)
				assert.Empty(t, runtime.authority.leases)
			}
		})
	}
}

func TestPowerUpUsesConfiguredResumeTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runtime := newReservationRuntime()
		ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
		runtime.authority.states["worker-0"] = "IDLE+POWERING_UP"
		go func() {
			time.Sleep(20 * time.Minute)
			a := runtime.authority
			a.mu.Lock()
			defer a.mu.Unlock()
			a.states["worker-0"] = "IDLE"
		}()
		start := time.Now()
		require.NoError(t, FinishWorkerReservation(ctx, runtime))
		assert.GreaterOrEqual(t, time.Since(start), 20*time.Minute)
		assert.Equal(t, 1, runtime.authority.counts["scontrol show config"])
	})
}

func TestCancellationAndConcurrentFinalization(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { assert.NoError(t, FinishWorkerReservation(ctx, runtime)) })
	}
	wg.Wait()
	assert.Empty(t, runtime.authority.leases)
}

func TestHostGuardChecksNativeMembershipAndSiblings(t *testing.T) {
	runtime := newReservationRuntime()
	runtime.authority.sharedHost = true
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	require.ErrorContains(t, RequireReservedHost(ctx, NewKubectlClient(runtime), "shared-host"), "outside")
	require.Error(t, RequireReservedHost(ctx, NewKubectlClient(runtime), "missing"))
	require.Error(t, RequireReservedHost(ctx, NewKubectlClient(runtime), ""))
	id, _ := WorkerReservationName(ctx)
	lease := runtime.authority.leases[id]
	lease.Nodes = []string{"worker-1"}
	runtime.authority.leases[id] = lease
	require.ErrorContains(t, RequireReservedHost(ctx, NewKubectlClient(runtime), "shared-host"), "unexpected membership")
	require.Error(t, FinishWorkerReservation(ctx, runtime))
}

func TestDeletionFailureRetainsLease(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	runtime.authority.deleteErr = errors.New("permission denied")
	require.Error(t, FinishWorkerReservation(ctx, runtime))
	require.Len(t, runtime.authority.leases, 1)
}

func TestInFlightSettlementIsBoundedAndRetainsLease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runtime := newReservationRuntime()
		ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
		done, err := workerReservationFromContext(ctx).beginSubmission()
		require.NoError(t, err)
		start := time.Now()
		err = FinishWorkerReservation(ctx, runtime)
		require.ErrorContains(t, err, "settle reservation submissions")
		assert.Equal(t, reservationOperationTimeout, time.Since(start))
		require.Len(t, runtime.authority.leases, 1)
		done()
	})
}

func TestRecoveryWaitsForFixedLogicalWorkerToReturn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runtime := newReservationRuntime()
		ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
		id, _ := WorkerReservationName(ctx)
		delete(runtime.authority.states, "worker-0")
		go func() {
			time.Sleep(time.Minute)
			a := runtime.authority
			a.mu.Lock()
			defer a.mu.Unlock()
			a.states["worker-0"] = "IDLE"
			assert.Equal(t, []string{"worker-0"}, a.leases[id].Nodes)
		}()
		require.NoError(t, FinishWorkerReservation(ctx, runtime))
		assert.Empty(t, runtime.authority.leases)
	})
}

func TestAcquisitionDoesNotOutliveItsLease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runtime := newReservationRuntime()
		ctx := WithReservationOptions(t.Context(), ReservationOptions{LeaseDuration: 30 * time.Minute, JobTimeLimit: 5 * time.Minute, JobDrainTimeout: time.Minute, RecoveryTimeout: time.Minute})
		ctx = WithWorkerReservation(ctx, NewReservationManager(), "short lease")
		runtime.authority.afterCreate = func() { runtime.authority.podReady = false }
		_, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(ctx, WorkerRequirements{Count: 1})
		require.ErrorContains(t, err, "too near expiry")
		runtime.authority.podReady = true
		require.NoError(t, FinishWorkerReservation(ctx, runtime))
		assert.Empty(t, runtime.authority.leases)
	})
}

func TestLatePowerTransitionsReceiveFullRecoveryBudget(t *testing.T) {
	for _, alreadyResuming := range []bool{false, true} {
		t.Run(fmt.Sprint(alreadyResuming), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				runtime := newReservationRuntime()
				ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0", "worker-1")
				if alreadyResuming {
					runtime.authority.states["worker-0"] = "IDLE+POWERING_UP"
				} else {
					runtime.authority.states["worker-0"] = "IDLE+COMPLETING"
				}
				resumed := make(chan struct{})
				go func() {
					defer close(resumed)
					time.Sleep(4 * time.Minute)
					a := runtime.authority
					a.mu.Lock()
					a.states["worker-0"] = "IDLE"
					a.states["worker-1"] = "IDLE+POWERING_UP"
					a.podReady = true
					a.mu.Unlock()
					time.Sleep(29 * time.Minute)
					a.mu.Lock()
					defer a.mu.Unlock()
					a.states["worker-1"] = "IDLE"
				}()
				start := time.Now()
				err := FinishWorkerReservation(ctx, runtime)
				elapsed := time.Since(start)
				<-resumed
				require.NoError(t, err)
				assert.GreaterOrEqual(t, elapsed, 33*time.Minute)
				assert.Empty(t, runtime.authority.leases)
				assert.Equal(t, 1, runtime.authority.counts["scontrol show config"], "share the configured allowance across workers")
			})
		})
	}
}
