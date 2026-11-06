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

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reservationRuntime struct {
	Runtime
	mu         sync.Mutex
	states     map[string][]string
	hostReady  bool
	podReady   bool
	sharedHost bool
	nodeSets   string
	jobs       map[string]bool
	cancelled  []string
	cancelErr  map[string]error
	onNodeRead func()
	commands   []string
}

func newReservationRuntime() *reservationRuntime {
	return &reservationRuntime{
		states:    map[string][]string{"worker-0": {"IDLE"}, "worker-1": {"IDLE"}},
		hostReady: true, podReady: true,
		nodeSets: `{"items":[{"metadata":{"name":"worker"},"spec":{"replicas":2,"docker":{"enabled":false}}}]}`,
		jobs:     make(map[string]bool), cancelErr: make(map[string]error),
	}
}

func (r *reservationRuntime) Controller() CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if strings.HasPrefix(command, "sinfo ") {
			return "worker-0\nworker-1", nil
		}
		if r.onNodeRead != nil {
			r.onNodeRead()
		}
		var lines []string
		for _, name := range []string{"worker-0", "worker-1"} {
			states := r.states[name]
			if len(states) == 0 {
				continue
			}
			lines = append(lines, "NodeName="+name+" State="+states[0])
			if len(states) > 1 {
				r.states[name] = states[1:]
			}
		}
		return strings.Join(lines, "\n"), nil
	})
}

func (r *reservationRuntime) Kubectl() ArgsScope {
	return NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		switch strings.Join(args[:2], " ") {
		case "get nodesets":
			return r.nodeSets, nil
		case "get pods":
			status := "False"
			if r.podReady {
				status = "True"
			}
			secondHost := "host-1"
			if r.sharedHost {
				secondHost = "host-0"
			}
			return fmt.Sprintf(`{"items":[
				{"metadata":{"name":"worker-0"},"spec":{"hostname":"worker-0","nodeName":"host-0"},"status":{"conditions":[{"type":"Ready","status":%q}]}},
				{"metadata":{"name":"worker-1"},"spec":{"hostname":"worker-1","nodeName":%q},"status":{"conditions":[{"type":"Ready","status":%q}]}}
			]}`, status, secondHost, status), nil
		case "get nodes":
			status := "False"
			if r.hostReady {
				status = "True"
			}
			return fmt.Sprintf(`{"items":[
				{"metadata":{"name":"host-0"},"status":{"conditions":[{"type":"Ready","status":%q}]}},
				{"metadata":{"name":"host-1"},"status":{"conditions":[{"type":"Ready","status":%q}]}}
			]}`, status, status), nil
		default:
			return "", fmt.Errorf("unexpected kubectl arguments %v", args)
		}
	})
}

type reservationTestCommandScope struct{ CommandScope }

func (s reservationTestCommandScope) RunWithDefaultRetry(ctx context.Context, command string) (string, error) {
	return s.Run(ctx, command)
}

func (r *reservationRuntime) Jail() CommandScope {
	return reservationTestCommandScope{NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.commands = append(r.commands, command)
		for id := range r.jobs {
			if command == "scancel "+ShellQuote(id) {
				r.cancelled = append(r.cancelled, id)
				if err := r.cancelErr[id]; err != nil {
					return "", err
				}
				r.jobs[id] = false
				return "", nil
			}
			if strings.HasPrefix(command, "squeue -h -j "+ShellQuote(id)+" ") {
				if r.jobs[id] {
					return "RUNNING", nil
				}
				return "", nil
			}
		}
		if strings.Contains(command, "sbatch ") {
			r.jobs["123"] = true
			return "123", nil
		}
		return "", fmt.Errorf("unexpected jail command %q", command)
	})}
}

func (*reservationRuntime) Logf(string, ...any) {}

func (*reservationRuntime) WaitFor(ctx context.Context, description string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	var lastErr error
	for range 4 {
		if err := ctx.Err(); err != nil {
			return err
		}
		done, err := condition(ctx)
		if err == nil && done {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("wait for %s: %w", description, lastErr)
}

func acquireForFinalization(t *testing.T, pool *WorkerPool) context.Context {
	t.Helper()
	ctx := WithWorkerReservation(t.Context(), pool, t.Name())
	_, err := workerReservationFromContext(ctx).acquire(ctx, 1, fixedCandidates("worker-0"))
	require.NoError(t, err)
	return ctx
}

func TestFinishWorkerReservationCleansOwnedJobsAfterCleanupError(t *testing.T) {
	pool := NewWorkerPool()
	ctx := acquireForFinalization(t, pool)
	runtime := newReservationRuntime()
	runtime.jobs = map[string]bool{"123": true, "999": true}
	TrackReservedJob(ctx, "123")
	cleanupErr := errors.New("restore configuration")
	RecordWorkerCleanupError(ctx, cleanupErr)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, FinishWorkerReservation(cancelled, runtime), cleanupErr)
	assert.Equal(t, []string{"123"}, runtime.cancelled)
	assert.True(t, runtime.jobs["999"], "another scenario's job must remain running")
	assert.ErrorIs(t, pool.quarantine["worker-0"], cleanupErr)
	assert.Empty(t, pool.owners)
	require.ErrorIs(t, FinishWorkerReservation(ctx, runtime), cleanupErr)
	assert.Len(t, runtime.cancelled, 1, "finalization must be idempotent")
}

func TestFinishWorkerReservationAttemptsOtherJobsAfterCancellationFailure(t *testing.T) {
	pool := NewWorkerPool()
	ctx := acquireForFinalization(t, pool)
	runtime := newReservationRuntime()
	runtime.jobs = map[string]bool{"123": true, "456": true}
	runtime.cancelErr["123"] = errors.New("permission denied")
	TrackReservedJob(ctx, "123")
	TrackReservedJob(ctx, "456")
	err := FinishWorkerReservation(ctx, runtime)
	require.ErrorContains(t, err, "permission denied")
	assert.Equal(t, []string{"123", "456"}, runtime.cancelled)
	assert.False(t, runtime.jobs["456"])
	assert.Contains(t, pool.quarantine, "worker-0")
}

func TestFinishWorkerReservationWaitsForEpilogAndChecksPodAndHost(t *testing.T) {
	for _, tt := range []struct {
		name      string
		podReady  bool
		hostReady bool
		states    []string
		wantErr   bool
	}{
		{name: "epilog", podReady: true, hostReady: true, states: []string{"IDLE+CLOUD+COMPLETING", "IDLE+CLOUD"}},
		{name: "pod unready", hostReady: true, wantErr: true},
		{name: "host unready", podReady: true, wantErr: true},
		{name: "healthy power save", states: []string{"IDLE+CLOUD+POWERED_DOWN"}},
		{name: "power transition", states: []string{"IDLE+CLOUD+POWERING_UP"}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewWorkerPool()
			ctx := acquireForFinalization(t, pool)
			runtime := newReservationRuntime()
			runtime.podReady, runtime.hostReady = tt.podReady, tt.hostReady
			if len(tt.states) != 0 {
				runtime.states["worker-0"] = slices.Clone(tt.states)
			}
			reads := 0
			runtime.onNodeRead = func() {
				reads++
				pool.mu.Lock()
				defer pool.mu.Unlock()
				assert.NotNil(t, pool.owners["worker-0"], "hold ownership throughout recovery and epilog")
			}
			err := FinishWorkerReservation(ctx, runtime)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, pool.quarantine, "worker-0")
			} else {
				require.NoError(t, err)
				assert.Empty(t, pool.quarantine)
			}
			if tt.name == "epilog" {
				assert.Equal(t, 2, reads)
			}
			assert.Empty(t, pool.owners)
		})
	}
}

func TestFinishWorkerReservationCancelsPendingAcquisition(t *testing.T) {
	pool := NewWorkerPool()
	ctx := WithWorkerReservation(t.Context(), pool, "pending")
	r := workerReservationFromContext(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := r.acquire(ctx, 1, func(context.Context) (workerDiscovery, error) {
			candidates := testCandidates("worker-0")
			candidates[0].reason = "host not Ready"
			return workerDiscovery{candidates: candidates, configured: len(candidates)}, nil
		})
		done <- err
	}()
	waitForWorkerRequests(t, pool, 1)
	require.NoError(t, FinishWorkerReservation(ctx, nil))
	require.ErrorIs(t, <-done, context.Canceled)
	assert.Empty(t, pool.waiting)
	assert.Empty(t, pool.owners)
	_, err := r.acquire(ctx, 1, fixedCandidates("worker-0"))
	require.ErrorContains(t, err, "complete set once")
}

func TestWorkerAcquisitionIsOptInAndPreservesLegacySelection(t *testing.T) {
	runtime := newReservationRuntime()
	selector := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator")
	_, err := selector.Acquire(t.Context(), WorkerRequirements{Count: 1})
	require.ErrorContains(t, err, "ownership scope is required")
	workers, err := selector.PickWorkers(t.Context(), 1)
	require.NoError(t, err)
	assert.Len(t, workers, 1)
	job, err := NewSlurmClient(runtime).SubmitBatch(t.Context(), SbatchOptions{JobName: "legacy", Wrap: "true"})
	require.NoError(t, err)
	assert.Equal(t, "123", job.ID)
	require.NoError(t, FinishWorkerReservation(t.Context(), runtime))
	assert.Empty(t, runtime.cancelled)
}

func TestWorkerAcquisitionFiltersAndSkipVersusFailure(t *testing.T) {
	for _, tt := range []struct {
		name  string
		needs WorkerRequirements
		skip  bool
	}{
		{name: "GPU absent", needs: WorkerRequirements{Kind: "GPU", Count: 1}, skip: true},
		{name: "capacity absent", needs: WorkerRequirements{Count: 3}, skip: true},
		{name: "NodeSet absent", needs: WorkerRequirements{NodeSet: "other", Count: 1}, skip: true},
		{name: "Docker absent", needs: WorkerRequirements{Docker: true, Count: 1}, skip: true},
		{name: "configured but unready", needs: WorkerRequirements{Names: []string{"worker-0"}, Count: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime := newReservationRuntime()
			runtime.hostReady = false
			selector := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator")
			pool := NewWorkerPool()
			ctx := WithWorkerAcquireTimeout(WithWorkerReservation(t.Context(), pool, tt.name), 10*time.Millisecond)
			_, err := selector.Acquire(ctx, tt.needs)
			if tt.skip {
				assert.ErrorIs(t, SkipIfInsufficientWorkers(nil, err), godog.ErrSkip)
			} else {
				assert.ErrorIs(t, err, context.DeadlineExceeded)
				assert.NotErrorIs(t, SkipIfInsufficientWorkers(nil, err), godog.ErrSkip)
			}
			assert.Empty(t, pool.owners)
			assert.Empty(t, pool.waiting)
		})
	}
}

func TestScopedSelectionAndBatchJobsStayInsideReservation(t *testing.T) {
	runtime := newReservationRuntime()
	selector := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator")
	pool := NewWorkerPool()
	ctx := WithWorkerReservation(t.Context(), pool, "first")
	workers, err := selector.Acquire(ctx, WorkerRequirements{Count: 1, Names: []string{"worker-0"}, Idle: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"worker-0"}, WorkerNames(workers))
	other := WithWorkerReservation(t.Context(), pool, "other")
	otherWorkers, err := selector.PickCPUWorkers(other, 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"worker-1"}, WorkerNames(otherWorkers))
	slurm := NewSlurmClient(runtime)
	_, err = slurm.SubmitBatch(ctx, SbatchOptions{JobName: "missing nodelist", Wrap: "true"})
	require.ErrorContains(t, err, "explicit nodelist")
	_, err = slurm.SubmitBatch(ctx, SbatchOptions{JobName: "wrong worker", Wrap: "true", Nodelist: []string{"worker-1"}})
	require.ErrorContains(t, err, "outside the scenario reservation")
	_, err = slurm.SubmitBatch(ctx, SbatchOptions{JobName: "too many nodes", Wrap: "true", Nodelist: []string{"worker-0"}, Nodes: 2})
	require.ErrorContains(t, err, "node count")
	job, err := slurm.SubmitBatch(ctx, SbatchOptions{JobName: "owned", Wrap: "true", Nodelist: []string{"worker-0"}})
	require.NoError(t, err)
	assert.Equal(t, "123", job.ID)
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
	assert.Equal(t, []string{"123"}, runtime.cancelled)
	require.NoError(t, FinishWorkerReservation(other, runtime))
}

func TestRequireReservedHostRejectsUnownedSibling(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := acquireForFinalization(t, NewWorkerPool())
	kubectl := NewKubectlClient(runtime)
	require.NoError(t, RequireReservedHost(ctx, kubectl, "host-0"))
	runtime.sharedHost = true
	err := RequireReservedHost(ctx, kubectl, "host-0")
	require.ErrorContains(t, err, "outside the scenario reservation")
	require.Error(t, RequireReservedHost(ctx, kubectl, "missing"))
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
}

func TestFinishWorkerReservationIsConcurrentAndIdempotent(t *testing.T) {
	pool := NewWorkerPool()
	ctx := acquireForFinalization(t, pool)
	runtime := newReservationRuntime()
	runtime.jobs["123"] = true
	TrackReservedJob(ctx, "123")
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- FinishWorkerReservation(ctx, runtime) }()
	}
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	assert.Equal(t, []string{"123"}, runtime.cancelled)
	assert.Empty(t, pool.owners)
}

func TestWorkerAcquisitionWaitsForHostRecovery(t *testing.T) {
	runtime := newReservationRuntime()
	runtime.hostReady = false
	pool := NewWorkerPool()
	ctx, cancel := context.WithTimeout(WithWorkerReservation(t.Context(), pool, "host recovery"), 3*time.Second)
	defer cancel()
	selector := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator")
	done := make(chan error, 1)
	go func() {
		_, err := selector.Acquire(ctx, WorkerRequirements{Count: 1, Names: []string{"worker-0"}})
		done <- err
	}()
	waitForWorkerRequests(t, pool, 1)
	pool.mu.Lock()
	assert.Empty(t, pool.owners)
	pool.mu.Unlock()
	runtime.mu.Lock()
	runtime.hostReady = true
	runtime.mu.Unlock()
	pool.mu.Lock()
	pool.notifyLocked()
	pool.mu.Unlock()
	require.NoError(t, <-done)
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
}

func TestWorkerAcquisitionRejectsInvalidSelections(t *testing.T) {
	runtime := newReservationRuntime()
	selector := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator")
	ctx := WithWorkerReservation(t.Context(), NewWorkerPool(), "invalid")
	for _, needs := range []WorkerRequirements{
		{}, {Count: 1, Kind: "invalid"}, {Count: 1, Names: []string{"worker-0", "worker-0"}},
		{Count: 1, Names: []string{"bad/name"}}, {Count: 2, Names: []string{"worker-0"}},
	} {
		_, err := selector.Acquire(ctx, needs)
		require.Error(t, err)
	}
	require.Error(t, selector.Reserve(ctx, WorkerRequirements{Count: 1}))
	require.NoError(t, selector.Reserve(ctx, WorkerRequirements{Count: 1, Names: []string{"worker-0"}}))
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
}

func TestWorkerReservationDefaultBudgets(t *testing.T) {
	assert.Equal(t, 2*time.Hour, WorkerAcquireTimeout(t.Context()))
	assert.Equal(t, 6*time.Minute, workerCleanupTimeout(t.Context()))
	assert.Equal(t, time.Minute, WorkerAcquireTimeout(WithWorkerAcquireTimeout(t.Context(), time.Minute)))
	assert.Equal(t, time.Minute, workerCleanupTimeout(WithWorkerCleanupTimeout(t.Context(), time.Minute)))
	assert.Equal(t, DefaultWorkerAcquireTimeout, WorkerAcquireTimeout(WithWorkerAcquireTimeout(t.Context(), 0)))
}
