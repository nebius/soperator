package framework

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReservations struct {
	mu                 sync.Mutex
	leases             map[string][]string
	jobs               map[string][]string
	createErr          error
	lostCreateResponse bool
	cancelAfterCreate  context.CancelFunc
	deleteErr          error
	cancelled          []string
	users              []string
	commands           []string
}

func newFakeReservations() *fakeReservations {
	return &fakeReservations{leases: make(map[string][]string), jobs: make(map[string][]string)}
}

func (f *fakeReservations) reservations(context.Context) (map[string][]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make(map[string][]string, len(f.leases))
	for name, workers := range f.leases {
		result[name] = slices.Clone(workers)
	}
	return result, nil
}

func (f *fakeReservations) createReservation(_ context.Context, name string, workers []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	for _, owned := range f.leases {
		for _, worker := range workers {
			if slices.Contains(owned, worker) {
				return errors.New("requested nodes are busy")
			}
		}
	}
	f.leases[name] = slices.Clone(workers)
	if f.cancelAfterCreate != nil {
		f.cancelAfterCreate()
	}
	if f.lostCreateResponse {
		return errors.New("EOF after create")
	}
	return nil
}

func (f *fakeReservations) deleteReservation(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.leases, name)
	return nil
}

func (f *fakeReservations) reservationJobs(_ context.Context, name string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.jobs[name]), nil
}

func (f *fakeReservations) cancelReservationJobs(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = append(f.cancelled, name)
	delete(f.jobs, name)
	return nil
}

func (f *fakeReservations) addReservationUser(_ context.Context, _, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users = append(f.users, user)
	return nil
}

// Exercise the real Slurm adapter through the same CommandScope as production.
func (f *fakeReservations) command(ctx context.Context, command string) (string, error) {
	f.mu.Lock()
	f.commands = append(f.commands, command)
	f.mu.Unlock()
	value := func(key string) string {
		for _, field := range strings.Fields(command) {
			if suffix, ok := strings.CutPrefix(field, key+"="); ok {
				return strings.Trim(suffix, "'")
			}
		}
		return ""
	}
	switch {
	case command == "scontrol -o show reservations":
		leases, _ := f.reservations(ctx)
		var lines []string
		for name, workers := range leases {
			lines = append(lines, "ReservationName="+name+" Nodes="+strings.Join(workers, ","))
		}
		return strings.Join(lines, "\n"), nil
	case strings.HasPrefix(command, "scontrol show hostnames "):
		return strings.ReplaceAll(strings.Trim(strings.TrimPrefix(command, "scontrol show hostnames "), "'"), ",", "\n"), nil
	case strings.HasPrefix(command, "scontrol create reservation "):
		return "", f.createReservation(ctx, value("ReservationName"), strings.Split(value("Nodes"), ","))
	case strings.HasPrefix(command, "scontrol delete "):
		return "", f.deleteReservation(ctx, value("ReservationName"))
	case strings.HasPrefix(command, "scontrol update ReservationName="):
		return "", nil
	case strings.HasPrefix(command, "squeue -h --reservation="):
		jobs, err := f.reservationJobs(ctx, value("--reservation"))
		return strings.Join(jobs, "\n"), err
	case strings.HasPrefix(command, "scancel --reservation="):
		return "", f.cancelReservationJobs(ctx, value("--reservation"))
	default:
		return "", fmt.Errorf("unexpected reservation command: %s", command)
	}
}

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

func fixedChoices(choices ...workerChoice) func(context.Context) ([]workerChoice, error) {
	return func(context.Context) ([]workerChoice, error) { return choices, nil }
}

func TestNativeReservationsArbitrateBetweenSeparatePoolsAndBackfill(t *testing.T) {
	backend := newFakeReservations()
	gpu := reservationWorkers("gpu0", "gpu1")
	choice := fixedChoices(workerChoice{count: 1, candidates: gpu})
	first := reservationForTest(NewWorkerPool(), "replacement")
	workers, err := first.acquireAny(t.Context(), backend, choice)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpu0"}, WorkerNames(workers))
	second := reservationForTest(NewWorkerPool(), "other process")
	workers, err = second.acquireAny(t.Context(), backend, choice)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpu1"}, WorkerNames(workers))
	blocked := reservationForTest(NewWorkerPool(), "both")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = blocked.acquireAny(ctx, backend, fixedChoices(workerChoice{count: 2, candidates: gpu}))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	leases, _ := backend.reservations(t.Context())
	assert.Len(t, leases, 2, "waiting must not create a partial reservation")
	require.NoError(t, backend.deleteReservation(t.Context(), first.id))
	require.NoError(t, backend.deleteReservation(t.Context(), second.id))
	workers, err = blocked.acquireAny(t.Context(), backend, fixedChoices(workerChoice{count: 2, candidates: gpu}))
	require.NoError(t, err)
	assert.Equal(t, []string{"gpu0", "gpu1"}, WorkerNames(workers))
}

func TestNativeReservationsAcquireConcurrentlyAcrossSeparatePools(t *testing.T) {
	backend := newFakeReservations()
	start := make(chan struct{})
	type outcome struct {
		workers []WorkerInfo
		err     error
	}
	results := make(chan outcome, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for range 2 {
		go func() {
			<-start
			r := reservationForTest(NewWorkerPool(), "independent runner")
			workers, err := r.acquireAny(ctx, backend, fixedChoices(workerChoice{count: 1, candidates: reservationWorkers("gpu0", "gpu1")}))
			results <- outcome{workers, err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Len(t, first.workers, 1)
	require.Len(t, second.workers, 1)
	assert.NotEqual(t, first.workers[0].Name, second.workers[0].Name)
}

func TestNativeReservationsPreferCompleteCPUSetAndFallBackImmediately(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(fmt.Sprint(busy), func(t *testing.T) {
			backend := newFakeReservations()
			if busy {
				backend.leases["other-runner"] = []string{"cpu-a0"}
			}
			r := reservationForTest(NewWorkerPool(), "rollout")
			workers, err := r.acquireAny(t.Context(), backend, fixedChoices(
				workerChoice{count: 2, candidates: reservationWorkers("gpu0", "gpu1")},
				workerChoice{count: 2, candidates: reservationWorkers("cpu-a0", "cpu-a1")},
			))
			require.NoError(t, err)
			want := []string{"cpu-a0", "cpu-a1"}
			if busy {
				want = []string{"gpu0", "gpu1"}
			}
			assert.Equal(t, want, WorkerNames(workers))
			_, err = r.acquireAny(t.Context(), backend, fixedChoices())
			require.ErrorContains(t, err, "already owns")
		})
	}
	backend := newFakeReservations()
	backend.leases["other-runner"] = []string{"cpu-a0"}
	r := reservationForTest(NewWorkerPool(), "another CPU set")
	workers, err := r.acquireAny(t.Context(), backend, fixedChoices(
		workerChoice{count: 2, candidates: reservationWorkers("gpu0", "gpu1")},
		workerChoice{count: 2, candidates: reservationWorkers("cpu-a0", "cpu-a1")},
		workerChoice{count: 2, candidates: reservationWorkers("cpu-b0", "cpu-b1")},
	))
	require.NoError(t, err)
	assert.Equal(t, []string{"cpu-b0", "cpu-b1"}, WorkerNames(workers))
}

func TestNativeReservationReconcilesLostCreateAndFailsOnPermissionError(t *testing.T) {
	backend := newFakeReservations()
	backend.lostCreateResponse = true
	r := reservationForTest(NewWorkerPool(), "lost response")
	_, err := r.acquireAny(t.Context(), backend, fixedChoices(workerChoice{count: 1, candidates: reservationWorkers("cpu0")}))
	require.NoError(t, err)
	backend.createErr = errors.New("access denied")
	another := reservationForTest(NewWorkerPool(), "not admin")
	_, err = another.acquireAny(t.Context(), backend, fixedChoices(workerChoice{count: 1, candidates: reservationWorkers("cpu1")}))
	require.ErrorContains(t, err, "access denied")
}

func TestNativeReservationRejectsOccupiedAndUnhealthyWorkers(t *testing.T) {
	for _, state := range []string{"ALLOCATED", "MIXED", "IDLE+COMPLETING", "IDLE+DRAIN", "IDLE+RESERVED", ""} {
		worker := reservationWorkers("cpu0")[0]
		worker.SlurmNode.State = state
		assert.False(t, reservableWorker(worker), state)
	}
}

func TestNativeReservationWaitsWhenNoIdleCandidateHasAppearedYet(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	r := reservationForTest(NewWorkerPool(), "idle filter currently empty")
	_, err := r.acquireAny(ctx, newFakeReservations(), fixedChoices(workerChoice{count: 1}))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotContains(t, err.Error(), "quarantined")
}

func TestCancelledNativeCreateKeepsLeaseForFinalCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	backend := newFakeReservations()
	backend.cancelAfterCreate = cancel
	r := reservationForTest(NewWorkerPool(), "cancelled during create")
	_, err := r.acquireAny(ctx, backend, fixedChoices(workerChoice{count: 1, candidates: reservationWorkers("cpu0")}))
	require.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, backend.leases, r.id)
	assert.NotNil(t, r.backend, "the final hook must retain the known native lease")
}

type reservationRecoveryRuntime struct {
	*workerReadinessRuntime
}

func (*reservationRecoveryRuntime) WaitFor(ctx context.Context, description string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	for range 3 {
		done, err := condition(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return fmt.Errorf("%s remains pending", description)
}

func TestNativeCleanupCancelsUnknownJobsAndRetainsUnsafeLease(t *testing.T) {
	for _, failCleanup := range []bool{false, true} {
		t.Run(fmt.Sprint(failCleanup), func(t *testing.T) {
			runtime := &reservationRecoveryRuntime{workerReadinessFixture()}
			runtime.hosts = func() string { return workerHostsFixture("True") }
			pool, backend := NewWorkerPool(), newFakeReservations()
			ctx := WithWorkerReservation(t.Context(), pool, "cleanup")
			r := workerReservation(ctx)
			_, err := r.acquireAny(ctx, backend, fixedChoices(workerChoice{count: 1, candidates: reservationWorkers("worker-0")}))
			require.NoError(t, err)
			backend.jobs[r.id] = []string{"unknown-submission-id"}
			if failCleanup {
				RecordWorkerCleanupError(ctx, errors.New("restore marker"))
			}
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			err = FinishWorkerReservation(cancelled, runtime)
			assert.Equal(t, []string{r.id}, backend.cancelled)
			if failCleanup {
				require.ErrorContains(t, err, "restore marker")
				assert.Contains(t, backend.leases, r.id)
				assert.Contains(t, pool.quarantine, "worker-0")
			} else {
				require.NoError(t, err)
				assert.Empty(t, backend.leases)
			}
			require.NoError(t, FinishWorkerReservation(ctx, runtime), "finalization must be idempotent")
		})
	}
}

func TestNativeCleanupRetainsLeaseWhenHostNotReadyOrDeleteFails(t *testing.T) {
	for _, hostReady := range []bool{false, true} {
		t.Run(fmt.Sprint(hostReady), func(t *testing.T) {
			runtime := &reservationRecoveryRuntime{workerReadinessFixture()}
			status := "False"
			if hostReady {
				status = "True"
			}
			runtime.hosts = func() string { return workerHostsFixture(status) }
			backend, pool := newFakeReservations(), NewWorkerPool()
			ctx := WithWorkerReservation(t.Context(), pool, "unsafe")
			r := workerReservation(ctx)
			_, err := r.acquireAny(ctx, backend, fixedChoices(workerChoice{count: 1, candidates: reservationWorkers("worker-0")}))
			require.NoError(t, err)
			if hostReady {
				backend.deleteErr = errors.New("delete rejected")
			}
			require.Error(t, FinishWorkerReservation(ctx, runtime))
			assert.Contains(t, backend.leases, r.id)
			assert.Contains(t, pool.quarantine, "worker-0")
		})
	}
}

func TestSequentialCleanupScopeDoesNotAcquireOrRouteJobs(t *testing.T) {
	ctx := WithWorkerCleanup(t.Context())
	assert.Nil(t, workerReservation(ctx))
	flags, err := WorkerReservationJobFlags(ctx, "test-user")
	require.NoError(t, err)
	assert.Empty(t, flags)
	assert.Equal(t, "srun hostname", reservationCommand(ctx, "srun hostname"))
	RecordWorkerCleanupError(ctx, errors.New("restore settings"))
	require.ErrorContains(t, FinishWorkerReservation(ctx, nil), "restore settings")
}
