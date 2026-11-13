package framework

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
)

func TestFailedAcquisitionValidationStillCleansLease(t *testing.T) {
	for _, mutation := range []string{"flags", "duration", "users"} {
		t.Run(mutation, func(t *testing.T) {
			runtime := newReservationRuntime()
			runtime.authority.afterCreate = func() {
				for id, lease := range runtime.authority.leases {
					switch mutation {
					case "flags":
						lease.Flags = []string{"SPEC_NODES"}
					case "duration":
						lease.Duration = "unrecognized display"
					case "users":
						lease.Users = nil
					}
					runtime.authority.leases[id] = lease
				}
			}
			ctx := WithWorkerReservation(t.Context(), NewReservationManager(), mutation)
			_, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(ctx, WorkerRequirements{Count: 1})
			require.Error(t, err)
			require.Len(t, runtime.authority.leases, 1)
			require.NoError(t, FinishWorkerReservation(ctx, runtime))
			assert.Empty(t, runtime.authority.leases)
		})
	}
}

func TestMembershipMismatchStillDrainsButRetainsLease(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	id, err := WorkerReservationName(ctx)
	require.NoError(t, err)
	lease := runtime.authority.leases[id]
	lease.Nodes = []string{"worker-1"}
	runtime.authority.leases[id] = lease
	runtime.authority.jobs[id] = []string{"17"}
	require.ErrorContains(t, FinishWorkerReservation(ctx, runtime), "unexpected membership")
	assert.Empty(t, runtime.authority.jobs)
	assert.Equal(t, 1, runtime.authority.cancelled)
	assert.Len(t, runtime.authority.leases, 1)
}

func TestAcquisitionWaitsThroughDiscoveryOutage(t *testing.T) {
	for _, recovers := range []bool{false, true} {
		t.Run(fmt.Sprint(recovers), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				runtime := newReservationRuntime()
				runtime.authority.readErr = kubectlCommandFailure("connection refused")
				ctx := WithWorkerAcquireTimeout(t.Context(), 10*time.Second)
				ctx = WithWorkerReservation(ctx, NewReservationManager(), "discovery outage")
				if recovers {
					go func() {
						time.Sleep(5 * time.Second)
						runtime.authority.mu.Lock()
						defer runtime.authority.mu.Unlock()
						runtime.authority.readErr = nil
					}()
				}
				start := time.Now()
				_, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(ctx, WorkerRequirements{Count: 1})
				if recovers {
					require.NoError(t, err)
					assert.GreaterOrEqual(t, time.Since(start), 5*time.Second)
				} else {
					require.ErrorIs(t, err, context.DeadlineExceeded)
					assert.Equal(t, 10*time.Second, time.Since(start))
					assert.Empty(t, runtime.authority.leases)
				}
				require.NoError(t, FinishWorkerReservation(ctx, runtime))
				// Shared reads intentionally outlive an individual waiter's deadline.
				cache := &workerReservationFromContext(ctx).manager.discovery
				cache.mu.Lock()
				var reads []<-chan struct{}
				for _, entry := range cache.entries {
					reads = append(reads, entry.done)
				}
				cache.mu.Unlock()
				for _, done := range reads {
					<-done
				}
			})
		})
	}
}

type reservationReadRuntime struct {
	Runtime
	controller CommandScope
}

func (r reservationReadRuntime) Controller() CommandScope { return r.controller }

func TestUnscopedWorkerDiscoveryPreservesLegacyRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		counts := make(map[string]int)
		runtime := reservationReadRuntime{controller: NewCommandScope(func(_ context.Context, command string) (string, error) {
			counts[command]++
			if strings.HasPrefix(command, "sinfo ") {
				if counts[command] < 5 {
					return "", kubectlCommandFailure("Unable to contact slurm controller (connect failure)")
				}
				return "worker-0", nil
			}
			if counts[command] < 5 {
				return "", kubectlCommandFailure("container not found")
			}
			return "NodeName=worker-0 State=IDLE", nil
		})}
		start := time.Now()
		workers, err := NewWorkerSelector(nil, NewSlurmClient(runtime), "soperator").Workers(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"worker-0"}, WorkerNames(workers))
		assert.Equal(t, 80*time.Second, time.Since(start))
		for _, count := range counts {
			assert.Equal(t, 5, count)
		}
	})
}

func TestScopedNodeReadsRetryTransientErrorsOnly(t *testing.T) {
	for _, temporary := range []bool{false, true} {
		t.Run(fmt.Sprint(temporary), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				reads := 0
				runtime := reservationReadRuntime{controller: NewCommandScope(func(context.Context, string) (string, error) {
					reads++
					if !temporary {
						return "", kubectlCommandFailure("Forbidden: controller exec")
					}
					if reads < 3 {
						return "", kubectlCommandFailure("Unable to contact slurm controller (connect failure)")
					}
					return "NodeName=worker-0 State=IDLE", nil
				})}
				ctx := WithWorkerReservation(t.Context(), NewReservationManager(), "node reads")
				start := time.Now()
				_, err := NewSlurmClient(runtime).NodeInfosWithRetries(ctx, []string{"worker-0"})
				if temporary {
					require.NoError(t, err)
					assert.Equal(t, 3, reads)
					assert.Equal(t, 2*time.Second, time.Since(start))
				} else {
					require.ErrorContains(t, err, "Forbidden")
					assert.Equal(t, 1, reads)
					assert.Zero(t, time.Since(start))
				}
			})
		})
	}
}

func TestCommandClassificationIgnoresArgv(t *testing.T) {
	for _, test := range []struct {
		stderr               string
		transient, rejection bool
	}{
		{stderr: "sbatch: error: Batch job submission failed: Invalid account", rejection: true},
		{stderr: "error: connection refused", transient: true},
		{stderr: "Unable to contact slurm controller (connect failure)", transient: true},
		{stderr: "container not found", transient: true},
		{stderr: "Error from server (Forbidden): nodes is forbidden"},
		{stderr: "Error from server (NotFound): nodeset missing"},
		{stderr: "sbatch: error: Batch job submission failed: Socket timed out", transient: true},
	} {
		t.Run(test.stderr, func(t *testing.T) {
			err := &CommandError{Name: "kubectl", Args: []string{"exec", "login", "--", "sbatch", "--wrap=timeout 60 nvidia-smi", "--output=/invalid/parse/dial tcp i/o timeout"}, Err: errors.New("exit status 1"), Stderr: test.stderr}
			assert.Equal(t, test.transient, transientDiscoveryError(fmt.Errorf("invalid wrapper: %w", err)))
			assert.Equal(t, test.rejection, definiteSbatchRejection(err))
			assert.False(t, isRetryableSbatchPreExecutionError("", err))
		})
	}
	assert.False(t, isRetryableSbatchPreExecutionError("", errors.New("kubectl exec payload dial tcp i/o timeout")), "opaque errors do not prove pre-execution failure")
	assert.False(t, transientDiscoveryError(errors.New("kubectl exec --wrap='connection refused'")))
	assert.False(t, definiteSbatchRejection(errors.New("kubectl exec --wrap='sbatch: error: Batch job submission failed: Invalid account'")))
}

func TestReservationAdapterAdmissionCoversExecution(t *testing.T) {
	runtime := newReservationRuntime()
	ctx := acquireNative(t, runtime, NewReservationManager(), "worker-0")
	entered, resume := make(chan struct{}), make(chan struct{})
	adapterDone := make(chan error, 1)
	go func() {
		adapterDone <- WithWorkerReservationJobFlags(ctx, "test-user", func(flags []string) error {
			close(entered)
			<-resume
			_, err := runtime.Jail().Run(ctx, "sbatch "+quoteArgs(flags)+" --wrap=true")
			return err
		})
	}()
	<-entered
	finished := make(chan error, 1)
	go func() { finished <- FinishWorkerReservation(ctx, runtime) }()
	r := workerReservationFromContext(ctx)
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.state == reservationFinalizing
	}, time.Second, time.Millisecond)
	select {
	case err := <-finished:
		t.Fatalf("finalized during adapter execution: %v", err)
	default:
	}
	require.Error(t, WithWorkerReservationJobFlags(ctx, "", func([]string) error {
		t.Fatal("admitted an adapter after finalization started")
		return nil
	}))
	close(resume)
	require.NoError(t, <-adapterDone)
	require.NoError(t, <-finished)
	assert.Equal(t, 1, runtime.authority.cancelled)
	assert.Empty(t, runtime.authority.leases)
}

func TestWorkerReadinessTargetsCurrentHosts(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var workers []WorkerInfo
			for i := range count {
				workers = append(workers, WorkerInfo{Name: fmt.Sprintf("worker-%d", i), NodeSetName: "gpu"})
			}
			for _, generation := range []string{"original", "replacement"} {
				reads := 0
				runtime := readinessRuntime{scope: NewArgsScope(func(_ context.Context, args ...string) (string, error) {
					reads++
					var records []string
					if args[1] == "pods" {
						assert.Equal(t, append([]string{"get", "pods", "-n", SoperatorNamespace, "--ignore-not-found", "-o", "json"}, WorkerNames(workers)...), args)
						for _, worker := range workers {
							records = append(records, fmt.Sprintf(`{"kind":"Pod","metadata":{"name":%q},"spec":{"hostname":%q,"nodeName":%q},"status":{"conditions":[{"type":"Ready","status":"True"}]}}`, worker.Name, worker.Name, generation+worker.Name))
						}
						if count == 1 {
							return records[0], nil
						}
					} else {
						expected := []string{"get", "nodes"}
						for _, worker := range workers {
							host := generation + worker.Name
							// A single name produces a Node, multiple names produce a List.
							records = append(records, fmt.Sprintf(`{"metadata":{"name":%q},"status":{"conditions":[{"type":"Ready","status":"True"}]}}`, host))
							expected = append(expected, host)
						}
						expected = append(expected, "--ignore-not-found", "-o", "json")
						assert.Equal(t, expected, args)
						if count == 1 {
							return records[0], nil
						}
					}
					return `{"items":[` + strings.Join(records, ",") + `]}`, nil
				})}
				kubectl := NewKubectlClient(runtime)
				pods, err := kubectl.WorkerPodsForSlurmNodes(t.Context(), WorkerNames(workers))
				require.NoError(t, err)
				ready, err := workerReadiness(t.Context(), kubectl, pods)
				require.NoError(t, err)
				for _, worker := range workers {
					assert.True(t, ready[worker.Name])
				}
				assert.Equal(t, 2, reads)
			}
		})
	}
}

type readinessRuntime struct {
	Runtime
	scope ArgsScope
}

func (r readinessRuntime) Kubectl() ArgsScope { return r.scope }

func TestNamedPodReadsUseScopedRetryPolicy(t *testing.T) {
	for _, temporary := range []bool{false, true} {
		t.Run(fmt.Sprint(temporary), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				reads := 0
				runtime := readinessRuntime{scope: NewArgsScope(func(context.Context, ...string) (string, error) {
					reads++
					if !temporary {
						return "", kubectlCommandFailure("Forbidden: pods")
					}
					if reads < 3 {
						return "", kubectlCommandFailure("connection refused")
					}
					return `{"kind":"Pod","metadata":{"name":"worker-0"},"spec":{"hostname":"worker-0"}}`, nil
				})}
				client := NewKubectlClient(runtime)
				client.retryTransientOnly = true
				start := time.Now()
				_, err := client.WorkerPodsForSlurmNodes(t.Context(), []string{"worker-0"})
				if temporary {
					require.NoError(t, err)
					assert.Equal(t, 3, reads)
					assert.Equal(t, 2*time.Second, time.Since(start))
				} else {
					require.ErrorContains(t, err, "Forbidden")
					assert.Equal(t, 1, reads)
					assert.Zero(t, time.Since(start))
				}
			})
		})
	}
}

func TestTargetedDiscoveryTreatsMissingWorkersAndHostsAsPending(t *testing.T) {
	runtime := reservationReadRuntime{controller: NewCommandScope(func(context.Context, string) (string, error) {
		return "Node worker-0 not found\n", kubectlCommandFailure("command terminated with exit code 1")
	})}
	_, err := NewSlurmClient(runtime).NodeInfosNoRetries(t.Context(), []string{"worker-0"})
	require.True(t, isPendingRecovery(err), "%v", err)
	kube := readinessRuntime{scope: NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		if args[1] == "pods" {
			return `{"kind":"Pod","metadata":{"name":"worker-0"},"spec":{"hostname":"worker-0","nodeName":"gone-host"}}`, nil
		}
		return "", nil // --ignore-not-found emits nothing for an absent named host.
	})}
	kubectl := NewKubectlClient(kube)
	pods, err := kubectl.WorkerPodsForSlurmNodes(t.Context(), []string{"worker-0"})
	require.NoError(t, err)
	ready, err := workerReadiness(t.Context(), kubectl, pods)
	require.NoError(t, err)
	assert.False(t, ready["worker-0"])
}

type confirmationFaultRuntime struct {
	*reservationRuntime
	failAt, failures int
	failure          error
	cancel           context.CancelFunc
	confirmations    []string
}

func (r *confirmationFaultRuntime) Controller() CommandScope {
	base := r.reservationRuntime.Controller()
	return NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if strings.HasPrefix(command, "scontrol -o show reservation ") {
			r.confirmations = append(r.confirmations, command)
			if len(r.confirmations) >= r.failAt && r.failures != 0 {
				if r.cancel != nil && len(r.confirmations) == r.failAt+2 {
					r.cancel()
				}
				if r.failures > 0 {
					r.failures--
				}
				return "", r.failure
			}
		}
		return base.Run(ctx, command)
	})
}

func TestAcquisitionReservationConfirmationRetries(t *testing.T) {
	for _, phase := range []struct {
		name   string
		failAt int
	}{{"after create", 1}, {"handoff", 2}} {
		for _, test := range []struct {
			name, stderr, message string
			failures              int
			wantErr               error
			elapsed               time.Duration
			cancel                bool
		}{
			{name: "recovers", stderr: "connection refused", failures: 3, elapsed: 3 * time.Second},
			{name: "deadline", stderr: "connection refused", failures: -1, wantErr: context.DeadlineExceeded, elapsed: 5 * time.Second},
			{name: "cancellation", stderr: "connection refused", failures: -1, wantErr: context.Canceled, elapsed: 2 * time.Second, cancel: true},
			{name: "permanent", stderr: "Forbidden: controller exec", failures: -1, message: "Forbidden"},
			{name: "absent", stderr: "Invalid reservation name", failures: -1, message: "reservation is absent"},
		} {
			t.Run(phase.name+"/"+test.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					runtime := &confirmationFaultRuntime{
						reservationRuntime: newReservationRuntime(), failAt: phase.failAt,
						failures: test.failures, failure: kubectlCommandFailure(test.stderr),
					}
					ctx, cancel := context.WithCancel(WithWorkerAcquireTimeout(t.Context(), 5*time.Second))
					defer cancel()
					ctx = WithWorkerReservation(ctx, NewReservationManager(), phase.name)
					if test.cancel {
						runtime.cancel = cancel
					}
					start := time.Now()
					workers, err := NewWorkerSelector(NewKubectlClient(runtime), NewSlurmClient(runtime), "soperator").Acquire(ctx, WorkerRequirements{Count: 1})
					if test.wantErr != nil {
						require.ErrorIs(t, err, test.wantErr)
					} else if test.message != "" {
						require.ErrorContains(t, err, test.message)
					} else {
						require.NoError(t, err)
						assert.Equal(t, []string{"worker-0"}, WorkerNames(workers))
						assert.Equal(t, phase.failAt+1, runtime.authority.counts["get pods"], "handoff retries must recheck readiness")
					}
					assert.Equal(t, test.elapsed, time.Since(start))
					creates := 0
					for command, count := range runtime.authority.counts {
						if strings.HasPrefix(command, "scontrol create reservation ") {
							creates += count
						}
					}
					assert.Equal(t, 1, creates)
					r := workerReservationFromContext(ctx)
					for _, command := range runtime.confirmations {
						assert.Equal(t, "scontrol -o show reservation "+ShellQuote(r.id), command)
					}
					assert.Len(t, runtime.authority.leases, 1, "keep ownership until independent cleanup")
					runtime.failures = 0
					require.NoError(t, FinishWorkerReservation(ctx, runtime))
					assert.Empty(t, runtime.authority.leases)
				})
			})
		}
	}
}
