package framework

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

const (
	// DefaultWorkerAcquireTimeout includes contention and readiness waits.
	DefaultWorkerAcquireTimeout = 2 * time.Hour
	// DefaultWorkerCleanupTimeout bounds final job cleanup and recovery checks.
	// Scenario-specific restoration runs before this final verification.
	DefaultWorkerCleanupTimeout = 6 * time.Minute
)

type workerReservationKey struct{}
type workerAcquireTimeoutKey struct{}
type workerCleanupTimeoutKey struct{}

// WithWorkerReservation creates an optional ownership scope. Call
// FinishWorkerReservation after all scenario cleanup, including on skip or
// failure. Stop scenario goroutines before finalizing. pool must be non-nil.
func WithWorkerReservation(ctx context.Context, pool *WorkerPool, name string) context.Context {
	if pool == nil {
		panic("worker reservation requires a pool")
	}
	return context.WithValue(ctx, workerReservationKey{}, &workerReservation{
		pool: pool, name: name, jobs: make(map[string]struct{}), finishDone: make(chan struct{}),
	})
}

func workerReservationFromContext(ctx context.Context) *workerReservation {
	r, _ := ctx.Value(workerReservationKey{}).(*workerReservation)
	return r
}

// WithWorkerAcquireTimeout overrides the acquisition budget. Nonpositive values
// use DefaultWorkerAcquireTimeout; an earlier context deadline still applies.
func WithWorkerAcquireTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, workerAcquireTimeoutKey{}, timeout)
}

func WorkerAcquireTimeout(ctx context.Context) time.Duration {
	if timeout, ok := ctx.Value(workerAcquireTimeoutKey{}).(time.Duration); ok && timeout > 0 {
		return timeout
	}
	return DefaultWorkerAcquireTimeout
}

// WithWorkerCleanupTimeout overrides final verification's independent budget.
// Nonpositive values use DefaultWorkerCleanupTimeout.
func WithWorkerCleanupTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, workerCleanupTimeoutKey{}, timeout)
}

func workerCleanupTimeout(ctx context.Context) time.Duration {
	if timeout, ok := ctx.Value(workerCleanupTimeoutKey{}).(time.Duration); ok && timeout > 0 {
		return timeout
	}
	return DefaultWorkerCleanupTimeout
}

// RecordWorkerCleanupError records an unsafe cleanup failure without replacing
// the original scenario error. Diagnostic artifact cleanup should only log.
func RecordWorkerCleanupError(ctx context.Context, err error) {
	if r := workerReservationFromContext(ctx); r != nil && err != nil {
		r.pool.mu.Lock()
		defer r.pool.mu.Unlock()
		r.cleanupErr = errors.Join(r.cleanupErr, err)
	}
}

// TrackReservedJob registers jobs launched indirectly, for example by an
// ActiveCheck. SubmitBatch tracks its own jobs automatically in scoped callers.
func TrackReservedJob(ctx context.Context, id string) {
	if r := workerReservationFromContext(ctx); r != nil && id != "" {
		r.pool.mu.Lock()
		defer r.pool.mu.Unlock()
		r.jobs[id] = struct{}{}
	}
}

func requireReservedWorkers(ctx context.Context, names []string) error {
	r := workerReservationFromContext(ctx)
	if r == nil {
		return nil
	}
	r.pool.mu.Lock()
	defer r.pool.mu.Unlock()
	if r.state != reservationOwned {
		return fmt.Errorf("use reserved workers: acquire workers before starting work")
	}
	if len(names) == 0 {
		return fmt.Errorf("use reserved workers: an explicit nodelist is required")
	}
	for _, name := range names {
		if r.pool.owners[name] != r {
			return fmt.Errorf("use reserved workers: worker %s is outside the scenario reservation", name)
		}
	}
	return nil
}

// RequireReservedHost checks ownership of every worker currently on the host
// before a host-wide disruption. Outside a scope it preserves legacy behavior.
func RequireReservedHost(ctx context.Context, kubectl *KubectlClient, nodeName string) error {
	if workerReservationFromContext(ctx) == nil {
		return nil
	}
	pods, err := kubectl.WorkerPods(ctx)
	if err != nil {
		return err
	}
	var names []string
	for _, pod := range pods {
		if pod.KubernetesNodeName == nodeName {
			names = append(names, pod.SlurmNodeName)
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("reserve host %s: no worker pods found", nodeName)
	}
	return requireReservedWorkers(ctx, names)
}

// FinishWorkerReservation retains ownership through owned-job cleanup, epilog
// completion and recovery. It runs independently of scenario cancellation,
// quarantines the allocation on failure, and returns the same result on repeat.
func FinishWorkerReservation(ctx context.Context, runtime Runtime) (result error) {
	r := workerReservationFromContext(ctx)
	if r == nil {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workerCleanupTimeout(ctx))
	defer cancel()
	p := r.pool
	p.mu.Lock()
	if r.state == reservationFinalizing || r.state == reservationFinished {
		p.mu.Unlock()
		select {
		case <-r.finishDone:
			return r.finishErr
		case <-cleanupCtx.Done():
			return cleanupCtx.Err()
		}
	}
	r.state = reservationFinalizing
	acquireDone, acquireCancel := r.acquireDone, r.acquireCancel
	p.mu.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			result = errors.Join(result, fmt.Errorf("finalize worker reservation %q: %v", r.name, recovered))
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		result = errors.Join(result, r.cleanupErr)
		r.finishErr = result
		r.state = reservationFinished
		p.releaseLocked(r, result)
		close(r.finishDone)
	}()
	if acquireCancel != nil {
		acquireCancel()
	}
	if acquireDone != nil {
		select {
		case <-acquireDone:
		case <-cleanupCtx.Done():
			return cleanupCtx.Err()
		}
	}
	p.mu.Lock()
	workers := slices.Clone(r.workers)
	var jobs []string
	for id := range r.jobs {
		jobs = append(jobs, id)
	}
	p.mu.Unlock()
	if len(workers) == 0 && len(jobs) == 0 {
		return nil
	}
	if runtime == nil {
		return fmt.Errorf("finalize worker reservation %q: runtime is required", r.name)
	}
	slices.Sort(jobs)
	slurm := NewSlurmClient(runtime)
	var failures []error
	for _, id := range jobs {
		if err := slurm.CancelJob(cleanupCtx, id, 0); err != nil {
			failures = append(failures, err)
		}
	}
	jobCtx, cancelJobs := context.WithTimeout(cleanupCtx, 2*time.Minute)
	defer cancelJobs()
	for _, id := range jobs {
		if err := slurm.WaitForJobGone(jobCtx, id, 2*time.Minute); err != nil {
			failures = append(failures, err)
		}
	}
	if len(workers) != 0 {
		if err := runtime.WaitFor(cleanupCtx, "reserved workers healthy after cleanup", workerCleanupTimeout(ctx), DefaultPollInterval,
			func(waitCtx context.Context) (bool, error) {
				return reservedWorkersRecovered(waitCtx, runtime, workers)
			}); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func reservedWorkersRecovered(ctx context.Context, runtime Runtime, workers []WorkerInfo) (bool, error) {
	ready, err := workerReadiness(ctx, NewKubectlClient(runtime))
	if err != nil {
		return false, err
	}
	nodes, err := NewSlurmClient(runtime).NodeInfosOnce(ctx, WorkerNames(workers))
	if err != nil {
		return false, err
	}
	for _, node := range nodes {
		if node.HasStateFlag("COMPLETING") || !node.IsHealthy() {
			return false, fmt.Errorf("recover worker %s: state=%s reason=%s", node.Name, node.State, node.Reason)
		}
		if node.HasStateFlag("POWERED_DOWN") {
			continue
		}
		if !node.IsReadyForImmediateWork() || !node.HasStateFlag("IDLE") || !ready[node.Name] {
			return false, fmt.Errorf("recover worker %s: state=%s pod_and_host_ready=%t", node.Name, node.State, ready[node.Name])
		}
	}
	return true, nil
}
