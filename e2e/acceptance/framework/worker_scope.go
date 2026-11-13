package framework

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const DefaultWorkerAcquireTimeout = 2 * time.Hour

// ReservationOptions applies to one opt-in scope. Zero durations use defaults.
// LeaseDuration includes scenario work, restoration and finalization. JobTimeLimit
// is a default, not a cap on an explicitly requested, compatible job duration.
type ReservationOptions struct {
	LeaseDuration   time.Duration
	JobTimeLimit    time.Duration
	JobDrainTimeout time.Duration
	RecoveryTimeout time.Duration
	// RequireActiveRecovery disallows the healthy powered-down baseline supported
	// by power-save profiles. Stronger disruptive recovery proofs belong to callers.
	RequireActiveRecovery bool
}

func (o ReservationOptions) defaults() ReservationOptions {
	// Current held rollout jobs allow 70m, and MK8s restoration alone allows 135m.
	// Leave room for both acquisition/readiness (2h) and work/restoration rather
	// than tying lease expiry to the default job time limit. Larger profiles override.
	if o.LeaseDuration == 0 {
		o.LeaseDuration = 8 * time.Hour
	}
	if o.JobTimeLimit == 0 {
		o.JobTimeLimit = 2 * time.Hour
	}
	if o.JobDrainTimeout == 0 {
		o.JobDrainTimeout = 15 * time.Minute
	}
	// Scenario restoration runs first; this verifies an already restored worker.
	if o.RecoveryTimeout == 0 {
		o.RecoveryTimeout = 5 * time.Minute
	}
	return o
}

func (o ReservationOptions) validate() error {
	if o.LeaseDuration <= 0 || o.JobTimeLimit <= 0 || o.JobDrainTimeout <= 0 || o.RecoveryTimeout <= 0 {
		return fmt.Errorf("configure reservation: durations must be positive")
	}
	if o.JobTimeLimit+o.JobDrainTimeout+o.RecoveryTimeout+reservationOperationTimeout >= o.LeaseDuration {
		return fmt.Errorf("configure reservation: lease must cover job time, drain, recovery and reconciliation")
	}
	return nil
}

type workerReservationKey struct{}
type workerAcquireTimeoutKey struct{}
type workerCleanupTimeoutKey struct{}
type reservationOptionsKey struct{}

// WithReservationOptions overrides profile defaults for subsequently created scopes.
func WithReservationOptions(ctx context.Context, options ReservationOptions) context.Context {
	return context.WithValue(ctx, reservationOptionsKey{}, options)
}

// WithWorkerReservation creates an optional native ownership scope. Scenario
// restoration and smoke verification must finish, and producers must stop,
// before FinishWorkerReservation closes submission admission.
func WithWorkerReservation(ctx context.Context, manager *ReservationManager, name string) context.Context {
	if manager == nil {
		panic("worker reservation requires a manager")
	}
	options, _ := ctx.Value(reservationOptionsKey{}).(ReservationOptions)
	return context.WithValue(ctx, workerReservationKey{}, &workerReservation{
		manager: manager, name: name, id: newReservationID(), options: options.defaults(),
		finishDone: make(chan struct{}), users: map[string]bool{"root": true, "": true},
	})
}

func workerReservationFromContext(ctx context.Context) *workerReservation {
	r, _ := ctx.Value(workerReservationKey{}).(*workerReservation)
	return r
}

func WithWorkerAcquireTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, workerAcquireTimeoutKey{}, timeout)
}
func WorkerAcquireTimeout(ctx context.Context) time.Duration {
	if timeout, ok := ctx.Value(workerAcquireTimeoutKey{}).(time.Duration); ok && timeout > 0 {
		return timeout
	}
	return DefaultWorkerAcquireTimeout
}

// WithWorkerCleanupTimeout optionally caps all finalization phases together.
// Without an override, drain and recovery have separate budgets. Restoration
// precedes finalization and is not shortened by this timeout.
func WithWorkerCleanupTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, workerCleanupTimeoutKey{}, timeout)
}

// RecordWorkerCleanupError records an unproved scenario restoration failure.
// A submission response error alone is not a restoration failure.
func RecordWorkerCleanupError(ctx context.Context, err error) {
	if r := workerReservationFromContext(ctx); r != nil && err != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.cleanupErr = errors.Join(r.cleanupErr, err)
	}
}

func requireReservedWorkers(ctx context.Context, names []string) error {
	r := workerReservationFromContext(ctx)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != reservationOwned || time.Until(r.expires) <= 0 {
		return fmt.Errorf("use reserved workers: acquire workers before starting work")
	}
	for _, name := range names {
		if !slices.ContainsFunc(r.workers, func(w WorkerInfo) bool { return w.Name == name }) {
			return fmt.Errorf("use reserved workers: worker %s is outside the scenario reservation", name)
		}
	}
	return nil
}

func (r *workerReservation) beginSubmission() (func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != reservationOwned || time.Until(r.expires) <= r.options.JobDrainTimeout+r.options.RecoveryTimeout+reservationOperationTimeout {
		return nil, fmt.Errorf("submit reserved job: reservation is closing or too near expiry")
	}
	if r.inFlight == 0 {
		r.submissionsDone = make(chan struct{})
	}
	r.inFlight++
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.inFlight--
		if r.inFlight == 0 {
			close(r.submissionsDone)
		}
	}, nil
}

// RequireReservedHost verifies native membership and all workers on the host
// before a host-wide disruption. Outside a scope it preserves legacy behavior.
func RequireReservedHost(ctx context.Context, kubectl *KubectlClient, nodeName string) error {
	r := workerReservationFromContext(ctx)
	if r == nil {
		return nil
	}
	if err := requireReservedWorkers(ctx, nil); err != nil {
		return err
	}
	if strings.TrimSpace(nodeName) == "" {
		return fmt.Errorf("reserve host: host name is empty")
	}
	observed, err := r.backend.get(ctx, r.id)
	if err != nil {
		return err
	}
	if err := r.validateObserved(observed); err != nil {
		return err
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

// FinishWorkerReservation closes admission after caller-owned restoration and
// producer shutdown, then drains and verifies under the live native lease.
// Cancellation does not bypass cleanup; repeat calls return the original result.
func FinishWorkerReservation(ctx context.Context, runtime Runtime) (result error) {
	r := workerReservationFromContext(ctx)
	if r == nil {
		return nil
	}
	cleanupCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	if timeout, _ := ctx.Value(workerCleanupTimeoutKey{}).(time.Duration); timeout > 0 {
		var timeoutCancel context.CancelFunc
		cleanupCtx, timeoutCancel = context.WithTimeout(cleanupCtx, timeout)
		defer timeoutCancel()
	}
	r.mu.Lock()
	if r.state == reservationFinalizing || r.state == reservationFinished {
		r.mu.Unlock()
		select {
		case <-r.finishDone:
			return r.finishErr
		case <-cleanupCtx.Done():
			return cleanupCtx.Err()
		}
	}
	handedOff := r.state == reservationOwned
	r.state = reservationFinalizing
	acquireCancel, acquireDone := r.acquireCancel, r.acquireDone
	r.mu.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			result = errors.Join(result, fmt.Errorf("finalize reservation %s: %v", r.id, recovered))
		}
		r.mu.Lock()
		result = errors.Join(result, r.cleanupErr)
		r.finishErr, r.state = result, reservationFinished
		retained := r.mayExist
		close(r.finishDone)
		r.mu.Unlock()
		if runtime != nil {
			runtime.Logf("Finish reservation=%s scenario=%q workers=%v expires=%s retained=%t error=%v", r.id, r.name, WorkerNames(r.workers), r.expires.Format(time.RFC3339), retained, result)
		}
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
	r.mu.Lock()
	submissionsDone, inFlight, mayExist, backend := r.submissionsDone, r.inFlight, r.mayExist, r.backend
	r.mu.Unlock()
	if !mayExist {
		return nil
	}
	if runtime == nil || backend == nil {
		return fmt.Errorf("finalize reservation %s: runtime and backend are required", r.id)
	}
	// Bound detached cleanup even if an execution adapter ignores the scenario deadline.
	leaseCtx, leaseCancel := context.WithDeadline(cleanupCtx, r.expires)
	defer leaseCancel()
	if inFlight > 0 {
		admissionCtx, admissionCancel := context.WithTimeout(leaseCtx, reservationOperationTimeout)
		defer admissionCancel()
		select {
		case <-submissionsDone:
		case <-admissionCtx.Done():
			return fmt.Errorf("settle reservation submissions: %w", admissionCtx.Err())
		}
	}
	observed, err := backend.get(leaseCtx, r.id)
	if err != nil {
		return err
	}
	if observed == nil {
		return fmt.Errorf("finalize reservation %s: reservation is absent before recovery proof (uncertain_create=%t)", r.id, r.uncertainCreate)
	}
	if err := r.validateObserved(observed); err != nil {
		runtime.Logf("Clean reservation %s despite acquisition validation error: %v", r.id, err)
	}
	ownershipErr := r.validateMembership(observed)
	if handedOff {
		ownershipErr = errors.Join(ownershipErr, r.validateOwnership(observed))
	}
	drainErr := r.drain(leaseCtx)
	recoveryErr := r.recover(leaseCtx, runtime)
	r.mu.Lock()
	restorationErr := r.cleanupErr
	r.mu.Unlock()
	if err := errors.Join(ownershipErr, drainErr, recoveryErr, restorationErr); err != nil {
		return err
	}
	// Recheck late jobs immediately before deletion. Slurm rejects deletion if a
	// submission races this read; it cannot subsequently allocate a deleted lease.
	jobs, err := backend.jobs(leaseCtx, r.id)
	if err != nil {
		return err
	}
	if len(jobs) != 0 {
		if err := r.drain(leaseCtx); err != nil {
			return err
		}
		if err := r.recover(leaseCtx, runtime); err != nil {
			return err
		}
	}
	observed, err = backend.get(leaseCtx, r.id)
	if err != nil {
		return err
	}
	if err := r.validateMembership(observed); err != nil {
		return err
	}
	if handedOff {
		if err := r.validateOwnership(observed); err != nil {
			return err
		}
	}
	deleteCtx, deleteCancel := context.WithTimeout(leaseCtx, reservationOperationTimeout)
	defer deleteCancel()
	if err := backend.delete(deleteCtx, r.id); err != nil {
		return err
	}
	r.mu.Lock()
	r.mayExist = false
	r.mu.Unlock()
	r.manager.invalidate()
	return nil
}

func (r *workerReservation) drain(ctx context.Context) error {
	drainCtx, cancel := context.WithTimeout(ctx, r.options.JobDrainTimeout)
	defer cancel()
	lastCancelled := ""
	for {
		jobs, err := r.backend.jobs(drainCtx, r.id)
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			return nil
		}
		slices.Sort(jobs)
		fingerprint := strings.Join(jobs, ",")
		if fingerprint != lastCancelled {
			if err := r.backend.cancelJobs(drainCtx, r.id); err != nil {
				return err
			}
			lastCancelled = fingerprint
		}
		if err := waitReservation(drainCtx, nil); err != nil {
			return fmt.Errorf("drain reservation %s jobs %v: %w", r.id, jobs, err)
		}
	}
}

type pendingRecoveryError struct{ problem string }

func (e *pendingRecoveryError) Error() string { return e.problem }
func isPendingRecovery(err error) bool {
	var pending *pendingRecoveryError
	return errors.As(err, &pending)
}

func pendingRecovery(format string, args ...any) error {
	return &pendingRecoveryError{problem: fmt.Sprintf(format, args...)}
}

func (r *workerReservation) recover(ctx context.Context, runtime Runtime) error {
	start := time.Now()
	deadline := start.Add(r.options.RecoveryTimeout)
	type transition struct{ worker, flag string }
	firstObserved := make(map[transition]time.Time)
	allowances := make(map[string]time.Duration)
	slurm := NewSlurmClient(runtime)
	var lastErr error
	for {
		probeCtx, probeCancel := context.WithDeadline(ctx, deadline)
		nodes, err := slurm.NodeInfosNoRetries(probeCtx, WorkerNames(r.workers))
		probeCancel()
		if err == nil {
			observedAt := time.Now()
			for _, node := range nodes {
				for _, flag := range []string{"POWERING_UP", "POWERING_DOWN", "POWER_DOWN"} {
					key := transition{worker: node.Name, flag: flag}
					if !node.HasStateFlag(flag) {
						continue
					}
					if _, seen := firstObserved[key]; seen {
						continue
					}
					firstObserved[key] = observedAt
					allowance, loaded := allowances[flag]
					if !loaded {
						configurationCtx, configurationCancel := context.WithTimeout(ctx, reservationOperationTimeout)
						var powerErr error
						allowance, powerErr = r.backend.powerTimeout(configurationCtx, flag)
						configurationCancel()
						if powerErr != nil {
							return powerErr
						}
						allowances[flag] = allowance
					}
					if candidate := observedAt.Add(allowance); candidate.After(deadline) {
						deadline = candidate
					}
				}
			}
			// A newly observed transition may extend this probe's original deadline.
			probeCtx, probeCancel = context.WithDeadline(ctx, deadline)
			err = recoveredWorkers(probeCtx, runtime, nodes, r.workers, r.options.RequireActiveRecovery)
		}
		probeCancel()
		if err == nil {
			return nil
		}
		if !isPendingRecovery(err) && !transientDiscoveryError(err) {
			return err
		}
		lastErr = err
		waitCtx, waitCancel := context.WithDeadline(ctx, deadline)
		err = waitReservation(waitCtx, nil)
		waitCancel()
		if err != nil {
			return fmt.Errorf("recover reservation %s: %w; last observation: %v", r.id, err, lastErr)
		}
	}
}

func recoveredWorkers(ctx context.Context, runtime Runtime, nodes []SlurmNodeInfo, workers []WorkerInfo, requireActive bool) error {
	for _, node := range nodes {
		if node.State == "" || !node.IsHealthy() || node.HasStateFlag("COMPLETING") || node.HasStateFlag("UNKNOWN") || node.HasStateFlag("FUTURE") {
			return pendingRecovery("recover worker %s state=%s reason=%s", node.Name, node.State, node.Reason)
		}
		if node.HasStateFlag("POWERED_DOWN") && !requireActive {
			continue
		}
		if !node.IsReadyForImmediateWork() {
			return pendingRecovery("recover worker %s state=%s", node.Name, node.State)
		}
	}
	kubectl := NewKubectlClient(runtime)
	kubectl.retryTransientOnly = true
	pods, err := kubectl.WorkerPodsForSlurmNodes(ctx, WorkerNames(workers))
	if err != nil {
		return err
	}
	ready, err := workerReadiness(ctx, kubectl, pods)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.HasStateFlag("POWERED_DOWN") && !requireActive {
			continue
		}
		if !ready[node.Name] {
			return pendingRecovery("recover worker %s pod or host is not Ready", node.Name)
		}
		if node.HasStateFlag("ALLOCATED") || node.HasStateFlag("MIXED") {
			output, err := discoveryRead(ctx, func(ctx context.Context) (string, error) {
				return runtime.Controller().Run(ctx, "squeue --all -h --nodelist="+ShellQuote(node.Name)+" -o '%i'")
			})
			if err != nil {
				return err
			}
			if strings.TrimSpace(output) != "" || node.CPUAlloc == nil || *node.CPUAlloc != 0 {
				return pendingRecovery("recover worker %s has unexplained occupancy: state=%s jobs=%s", node.Name, node.State, strings.TrimSpace(output))
			}
		}
	}
	return nil
}
