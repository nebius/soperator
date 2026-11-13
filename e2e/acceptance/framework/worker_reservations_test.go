package framework

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This authority is shared by otherwise independent runtimes/managers. All
// mutations are atomic here, just as they are at slurmctld, not in the manager.
type reservationAuthority struct {
	mu           sync.Mutex
	leases       map[string]slurmReservation
	jobs         map[string][]string
	states       map[string]string
	missingNodes map[string]bool
	counts       map[string]int
	calls        []string
	lostCreate   bool
	afterCreate  func()
	lostDelete   bool
	createErr    error
	deleteErr    error
	readErr      error
	cancelled    int
	keepJobs     bool
	podReady     bool
	hostReady    bool
	sharedHost   bool
	foreign      string
}

func newReservationRuntime() *reservationRuntime {
	return &reservationRuntime{authority: &reservationAuthority{
		leases: make(map[string]slurmReservation), jobs: make(map[string][]string),
		states: map[string]string{"worker-0": "IDLE", "worker-1": "IDLE"}, counts: make(map[string]int), podReady: true, hostReady: true,
	}}
}

type reservationRuntime struct {
	Runtime
	authority    *reservationAuthority
	submitErr    error
	submitOutput string
	prepErr      error
}

func commandValue(command, key string) string {
	for _, part := range strings.Fields(command) {
		part = strings.Trim(part, "'")
		if value, ok := strings.CutPrefix(part, key+"="); ok {
			return strings.Trim(value, "'")
		}
	}
	return ""
}

func (r *reservationRuntime) Controller() CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		a := r.authority
		a.mu.Lock()
		defer a.mu.Unlock()
		a.counts[command]++
		a.calls = append(a.calls, command)
		if a.readErr != nil && !strings.Contains(command, "create reservation") {
			return "", a.readErr
		}
		switch {
		case strings.HasPrefix(command, "sinfo "):
			var names []string
			for name := range a.states {
				names = append(names, name)
			}
			slices.Sort(names)
			return strings.Join(names, "\n"), nil
		case command == "scontrol -o show nodes", strings.HasPrefix(command, "scontrol -o show node "):
			var requested []string
			if value, found := strings.CutPrefix(command, "scontrol -o show node "); found {
				requested = strings.Split(strings.Trim(value, "'"), ",")
			}
			var lines []string
			for name, state := range a.states {
				if len(requested) != 0 && !slices.Contains(requested, name) || a.missingNodes[name] {
					continue
				}
				lines = append(lines, fmt.Sprintf("NodeName=%s State=%s CPUAlloc=0", name, state))
			}
			missing := false
			for _, name := range requested {
				if a.missingNodes[name] || a.states[name] == "" {
					lines = append(lines, "Node "+name+" not found")
					missing = true
				}
			}
			if missing {
				return strings.Join(lines, "\n"), kubectlCommandFailure("command terminated with exit code 1")
			}
			return strings.Join(lines, "\n"), nil
		case strings.HasPrefix(command, "scontrol create reservation"):
			if a.createErr != nil {
				return "", a.createErr
			}
			name, nodes := commandValue(command, "ReservationName"), strings.Split(commandValue(command, "Nodes"), ",")
			for _, lease := range a.leases {
				for _, node := range nodes {
					if slices.Contains(lease.Nodes, node) {
						return "", errors.New("reservation overlaps")
					}
				}
			}
			a.leases[name] = slurmReservation{Name: name, Nodes: nodes, Flags: []string{"SPEC_NODES", "STATIC"}, Users: []string{"root"}, EndTime: "later", Duration: commandValue(command, "Duration"), State: "ACTIVE"}
			if a.afterCreate != nil {
				a.afterCreate()
			}
			if a.lostCreate {
				return "", errors.New("unexpected EOF")
			}
			return "created", nil
		case strings.HasPrefix(command, "scontrol delete "):
			name := commandValue(command, "ReservationName")
			if a.deleteErr != nil {
				return "", a.deleteErr
			}
			if len(a.jobs[name]) > 0 {
				return "", errors.New("reservation has associated jobs")
			}
			delete(a.leases, name)
			if a.lostDelete {
				return "", errors.New("unexpected EOF")
			}
			return "", nil
		case strings.HasPrefix(command, "scontrol update "):
			name := commandValue(command, "ReservationName")
			lease := a.leases[name]
			lease.Users = append(lease.Users, commandValue(command, "Users+"))
			a.leases[name] = lease
			return "", nil
		case strings.HasPrefix(command, "scontrol show hostnames "):
			return strings.ReplaceAll(strings.Trim(strings.TrimPrefix(command, "scontrol show hostnames "), "'"), ",", "\n"), nil
		case strings.HasPrefix(command, "scontrol -o show reservation"):
			var leases []slurmReservation
			if command == "scontrol -o show reservations" {
				for _, lease := range a.leases {
					leases = append(leases, lease)
				}
			} else {
				name := strings.Trim(strings.TrimPrefix(command, "scontrol -o show reservation "), "'")
				lease, ok := a.leases[name]
				if !ok {
					return "Reservation " + name + " not found\n", errors.New("exit status 1")
				}
				leases = append(leases, lease)
			}
			if len(leases) == 0 {
				return "No reservations in the system", nil
			}
			var lines []string
			for _, lease := range leases {
				lines = append(lines, fmt.Sprintf("ReservationName=%s Nodes=%s Flags=%s Users=%s EndTime=%s Duration=%s State=%s", lease.Name, strings.Join(lease.Nodes, ","), strings.Join(lease.Flags, ","), strings.Join(lease.Users, ","), lease.EndTime, lease.Duration, lease.State))
			}
			return strings.Join(lines, "\n"), nil
		case strings.HasPrefix(command, "squeue "):
			if name := commandValue(command, "--reservation"); name != "" {
				return strings.Join(a.jobs[name], "\n"), nil
			}
			return a.foreign, nil
		case strings.HasPrefix(command, "scancel "):
			name := commandValue(command, "--reservation")
			a.cancelled++
			if !a.keepJobs {
				delete(a.jobs, name)
			}
			return "", nil
		case command == "scontrol show config":
			return "ResumeTimeout = 1800 sec\nSuspendTimeout = 90 sec", nil
		}
		return "", fmt.Errorf("unexpected controller command %s", command)
	})
}

func (r *reservationRuntime) Kubectl() ArgsScope {
	return NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		a := r.authority
		a.mu.Lock()
		defer a.mu.Unlock()
		key := strings.Join(args[:2], " ")
		a.counts[key]++
		if a.readErr != nil {
			return "", a.readErr
		}
		names := make([]string, 0, len(a.states))
		for name := range a.states {
			names = append(names, name)
		}
		slices.Sort(names)
		switch key {
		case "get nodesets":
			return fmt.Sprintf(`{"items":[{"metadata":{"name":"worker"},"spec":{"replicas":%d}}]}`, len(names)), nil
		case "get pods":
			if slices.Contains(args, "--ignore-not-found") {
				names = slices.DeleteFunc(names, func(name string) bool { return !slices.Contains(args[7:], name) })
			}
			var pods []string
			for _, name := range names {
				host := name + "-host"
				if a.sharedHost {
					host = "shared-host"
				}
				pods = append(pods, fmt.Sprintf(`{"kind":"Pod","metadata":{"name":%q},"spec":{"hostname":%q,"nodeName":%q},"status":{"conditions":[{"type":"Ready","status":%q}]}}`, name, name, host, readyString(a.podReady)))
			}
			if slices.Contains(args, "--ignore-not-found") && len(args[7:]) == 1 && len(pods) == 1 {
				return pods[0], nil
			}
			return `{"kind":"List","items":[` + strings.Join(pods, ",") + `]}`, nil
		case "get nodes":
			var nodes []string
			for _, name := range names {
				host := name + "-host"
				if a.sharedHost {
					host = "shared-host"
				}
				nodes = append(nodes, fmt.Sprintf(`{"metadata":{"name":%q},"status":{"conditions":[{"type":"Ready","status":%q}]}}`, host, readyString(a.hostReady)))
			}
			return `{"items":[` + strings.Join(nodes, ",") + `]}`, nil
		}
		return "", fmt.Errorf("unexpected kubectl arguments %v", args)
	})
}
func readyString(ready bool) string {
	if ready {
		return "True"
	}
	return "False"
}
func (r *reservationRuntime) Jail() CommandScope {
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		a := r.authority
		a.mu.Lock()
		defer a.mu.Unlock()
		a.calls = append(a.calls, command)
		if strings.Contains(command, "mkdir") {
			return "", r.prepErr
		}
		if strings.Contains(command, "sbatch") {
			name := commandValue(command, "--reservation")
			a.jobs[name] = append(a.jobs[name], "17")
			if r.submitErr != nil {
				return r.submitOutput, r.submitErr
			}
			return "17\n", nil
		}
		return "", nil
	})
}
func (*reservationRuntime) Logf(string, ...any) {}
func (*reservationRuntime) WaitFor(ctx context.Context, _ string, timeout, poll time.Duration, condition func(context.Context) (bool, error)) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		done, err := condition(waitCtx)
		if err != nil || done {
			return err
		}
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-time.After(poll):
		}
	}
}

func acquireNative(t *testing.T, runtime *reservationRuntime, manager *ReservationManager, names ...string) context.Context {
	t.Helper()
	ctx := WithWorkerReservation(t.Context(), manager, t.Name())
	selector := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator")
	_, err := selector.Acquire(ctx, WorkerRequirements{Count: len(names), Names: names})
	require.NoError(t, err)
	return ctx
}

func TestNativeReservationsIsolateIndependentRunners(t *testing.T) {
	runtime := newReservationRuntime()
	first := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	secondRuntime := &reservationRuntime{authority: runtime.authority}
	second := acquireNative(t, secondRuntime, NewReservationManager(), "worker-1")
	firstName, err := WorkerReservationName(first)
	require.NoError(t, err)
	secondName, err := WorkerReservationName(second)
	require.NoError(t, err)
	require.NotEqual(t, firstName, secondName)
	blocked, cancel := context.WithTimeout(WithWorkerReservation(t.Context(), NewReservationManager(), "contender"), 20*time.Millisecond)
	defer cancel()
	_, err = NewWorkerSelector(NewKubectlClient(secondRuntime), NewSlurmClient(secondRuntime), "soperator").Acquire(blocked, WorkerRequirements{Count: 2})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Len(t, runtime.authority.leases, 2)
	require.NoError(t, FinishWorkerReservation(first, runtime))
	require.NoError(t, FinishWorkerReservation(second, secondRuntime))
	assert.Empty(t, runtime.authority.leases)
}

func TestWaitingCompleteSetDoesNotBlockDisjointRequest(t *testing.T) {
	runtime := newReservationRuntime()
	runtime.authority.states["worker-2"] = "IDLE"
	manager := NewReservationManager()
	held := acquireNative(t, runtime, manager, "worker-0")
	waiting, cancel := context.WithTimeout(WithWorkerReservation(t.Context(), manager, "large"), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(waiting, WorkerRequirements{Count: 2, Names: []string{"worker-0", "worker-1"}})
		done <- err
	}()
	pending := workerReservationFromContext(waiting)
	require.Eventually(t, func() bool {
		pending.mu.Lock()
		defer pending.mu.Unlock()
		return pending.state == reservationAcquiring
	}, time.Second, time.Millisecond)
	independent := acquireNative(t, runtime, manager, "worker-2")
	require.NoError(t, FinishWorkerReservation(independent, runtime))
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.NoError(t, FinishWorkerReservation(held, runtime))
	require.NoError(t, FinishWorkerReservation(waiting, runtime))
}

func TestLostCreateAndDeleteResponsesAreReconciled(t *testing.T) {
	runtime := newReservationRuntime()
	runtime.authority.lostCreate = true
	runtime.authority.lostDelete = true
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0", "worker-1")
	require.Len(t, runtime.authority.leases, 1)
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
	assert.Empty(t, runtime.authority.leases)
}

func TestAcquisitionPermanentDiscoveryErrorFailsPromptly(t *testing.T) {
	runtime := newReservationRuntime()
	runtime.authority.readErr = errors.New("Forbidden: nodesets")
	ctx := WithWorkerReservation(t.Context(), NewReservationManager(), "forbidden")
	_, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(ctx, WorkerRequirements{Count: 1})
	require.ErrorContains(t, err, "Forbidden")
	assert.Equal(t, 1, runtime.authority.counts["get nodesets"])
	assert.Empty(t, runtime.authority.leases)
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
}

func TestDiscoveryAndHostExpansionAreShared(t *testing.T) {
	runtime := newReservationRuntime()
	manager := NewReservationManager()
	ctx := WithWorkerReservation(t.Context(), manager, "reads")
	r := workerReservationFromContext(ctx)
	r.backend = &slurmReservations{runtime: runtime, hosts: &manager.hosts}
	selector := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := selector.reservationCandidates(ctx, WorkerRequirements{Count: 1})
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	assert.Equal(t, 1, runtime.authority.counts["get nodesets"])
	backend := r.backend.(*slurmReservations)
	for range 3 {
		_, err := backend.parse(t.Context(), "ReservationName=other Nodes=worker-0,worker-1 Flags=SPEC_NODES,STATIC Users=root")
		require.NoError(t, err)
	}
	assert.Equal(t, 1, runtime.authority.counts["scontrol show hostnames 'worker-0,worker-1'"])
	manager.invalidate()
	_, err := selector.reservationCandidates(ctx, WorkerRequirements{Count: 1})
	require.NoError(t, err)
	assert.Equal(t, 2, runtime.authority.counts["get nodesets"])
}

func TestReadCacheCancellationDoesNotCancelOtherWaiters(t *testing.T) {
	var cache readCache[int]
	entered, release := make(chan struct{}), make(chan struct{})
	read := func(ctx context.Context) (int, error) {
		close(entered)
		select {
		case <-release:
			return 42, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := cache.get(ctx, "key", time.Second, read); done <- err }()
	<-entered
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	close(release)
	value, err := cache.get(t.Context(), "key", time.Second, read)
	require.NoError(t, err)
	assert.Equal(t, 42, value)
}

func TestCancellationAfterNativeCreationStillCleansLease(t *testing.T) {
	runtime := newReservationRuntime()
	ctx, cancel := context.WithCancel(WithWorkerReservation(t.Context(), NewReservationManager(), "cancel create"))
	defer cancel()
	runtime.authority.afterCreate = cancel
	_, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(ctx, WorkerRequirements{Count: 1})
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, runtime.authority.leases, 1)
	require.NoError(t, FinishWorkerReservation(ctx, runtime))
	assert.Empty(t, runtime.authority.leases)
}

func TestUncertainAbsentCreationIsNotResubmitted(t *testing.T) {
	runtime := newReservationRuntime()
	runtime.authority.createErr = errors.New("unexpected EOF")
	ctx := WithWorkerReservation(t.Context(), NewReservationManager(), "uncertain create")
	_, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(ctx, WorkerRequirements{Count: 1})
	require.Error(t, err)
	creates := 0
	for command, count := range runtime.authority.counts {
		if strings.Contains(command, "create reservation") {
			creates += count
		}
	}
	assert.Equal(t, 1, creates)
	require.ErrorIs(t, FinishWorkerReservation(ctx, runtime), errReservationAbsent)
	assert.True(t, workerReservationFromContext(ctx).mayExist)
}

func TestNativeMembershipFlagsAndLifetimeAreVerified(t *testing.T) {
	for _, mutation := range []string{"nodes", "flags", "duration"} {
		t.Run(mutation, func(t *testing.T) {
			runtime := newReservationRuntime()
			ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
			id, _ := WorkerReservationName(ctx)
			lease := runtime.authority.leases[id]
			switch mutation {
			case "nodes":
				lease.Nodes = []string{"worker-1"}
			case "flags":
				lease.Flags = append(lease.Flags, "FLEX")
			case "duration":
				lease.Duration = "00:01:00"
			}
			runtime.authority.leases[id] = lease
			require.Error(t, workerReservationFromContext(ctx).validateObserved(&lease))
			if mutation == "duration" {
				require.NoError(t, FinishWorkerReservation(ctx, runtime))
				require.Empty(t, runtime.authority.leases)
			} else {
				require.Error(t, FinishWorkerReservation(ctx, runtime))
				require.Len(t, runtime.authority.leases, 1)
			}
		})
	}
}

func TestAcquisitionRetriesTemporarilyMissingNodeRecord(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runtime := newReservationRuntime()
		runtime.authority.missingNodes = map[string]bool{"worker-0": true}
		ctx := WithWorkerReservation(t.Context(), NewReservationManager(), "transient disappearance")
		restored := make(chan struct{})
		go func() {
			defer close(restored)
			time.Sleep(2 * time.Second)
			a := runtime.authority
			a.mu.Lock()
			defer a.mu.Unlock()
			assert.Empty(t, a.leases, "waiting must not retain a partial reservation")
			delete(a.missingNodes, "worker-0")
		}()
		start := time.Now()
		workers, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(ctx, WorkerRequirements{Count: 2})
		<-restored
		require.NoError(t, err)
		assert.Equal(t, []string{"worker-0", "worker-1"}, WorkerNames(workers))
		assert.GreaterOrEqual(t, time.Since(start), 2*time.Second)
		require.NoError(t, FinishWorkerReservation(ctx, runtime))
	})
}

func TestAcquisitionMissingNodeRecordHonorsBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runtime := newReservationRuntime()
		runtime.authority.missingNodes = map[string]bool{"worker-0": true}
		ctx := WithWorkerAcquireTimeout(t.Context(), 3*time.Second)
		ctx = WithWorkerReservation(ctx, NewReservationManager(), "missing worker")
		_, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(ctx, WorkerRequirements{Count: 2})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Contains(t, err.Error(), "missing record for worker-0")
		assert.Empty(t, runtime.authority.leases)
		require.NoError(t, FinishWorkerReservation(ctx, runtime))
	})
}
