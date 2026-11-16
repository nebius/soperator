package framework

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// ReservationManager shares discovery reads and release notifications for one
// cluster. Slurm alone arbitrates ownership, including between independent runners.
type ReservationManager struct {
	discovery readCache[reservationDiscovery]
	hosts     readCache[[]string]
	mu        sync.Mutex
	changed   chan struct{}
}

type reservationDiscovery struct {
	snapshot WorkerSnapshot
	ready    map[string]bool
	occupied map[string]string
}

func NewReservationManager() *ReservationManager {
	return &ReservationManager{changed: make(chan struct{})}
}

func (m *ReservationManager) invalidate() {
	m.discovery.invalidate()
	m.mu.Lock()
	defer m.mu.Unlock()
	close(m.changed)
	m.changed = make(chan struct{})
}

func (m *ReservationManager) changes() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.changed
}

type reservationState uint8

const (
	reservationNew reservationState = iota
	reservationAcquiring
	reservationOwned
	reservationFinalizing
	reservationFinished
)

type workerReservation struct {
	mu              sync.Mutex
	manager         *ReservationManager
	name            string
	id              string
	state           reservationState
	options         ReservationOptions
	backend         reservationBackend
	workers         []WorkerInfo
	expires         time.Time
	mayExist        bool
	uncertainCreate bool
	cleanupErr      error
	acquireCancel   context.CancelFunc
	acquireDone     chan struct{}
	inFlight        int
	submissionsDone chan struct{}
	finishDone      chan struct{}
	finishErr       error
	users           map[string]bool
	userMu          sync.Mutex
}

type workerCandidate struct {
	worker WorkerInfo
	reason string
}

func (c workerCandidate) usable() bool {
	node := c.worker.SlurmNode
	return c.reason == "" && node.IsHealthy() && node.HasStateFlag("IDLE") &&
		!node.HasStateFlag("COMPLETING") && !node.HasStateFlag("ALLOCATED") && !node.HasStateFlag("MIXED") && !node.HasStateFlag("RESERVED") &&
		!node.HasStateFlag("POWERING_UP") && !node.HasStateFlag("POWERING_DOWN") && !node.HasStateFlag("POWER_DOWN")
}

type workerDiscovery struct {
	candidates []workerCandidate
	configured int
}

func (r *workerReservation) acquire(ctx context.Context, backend reservationBackend, count int, discover func(context.Context) (workerDiscovery, error), verify func(context.Context, []WorkerInfo) ([]WorkerInfo, error)) ([]WorkerInfo, error) {
	waitCtx, cancel := context.WithTimeout(ctx, WorkerAcquireTimeout(ctx))
	defer cancel()
	r.mu.Lock()
	if r.state != reservationNew {
		r.mu.Unlock()
		return nil, fmt.Errorf("reserve workers for %q: acquire the complete set once", r.name)
	}
	if err := r.options.validate(); err != nil {
		r.mu.Unlock()
		return nil, err
	}
	r.state, r.backend = reservationAcquiring, backend
	r.acquireCancel, r.acquireDone = cancel, make(chan struct{})
	r.mu.Unlock()
	defer func() { r.mu.Lock(); defer r.mu.Unlock(); close(r.acquireDone) }()
	var lastErr error
	for {
		if err := waitCtx.Err(); err != nil {
			return nil, fmt.Errorf("reserve workers for %q: %w; last observation: %v", r.name, err, lastErr)
		}
		changed := r.manager.changes()
		observation, err := discover(waitCtx)
		if err != nil {
			if !isPendingRecovery(err) && !transientDiscoveryError(err) {
				if waitCtx.Err() != nil {
					return nil, fmt.Errorf("reserve workers for %q: %w; last observation: %v", r.name, err, lastErr)
				}
				return nil, err
			}
			lastErr = err
			if waitErr := waitReservation(waitCtx, changed); waitErr != nil {
				return nil, fmt.Errorf("reserve workers for %q: %w; last observation: %v", r.name, waitErr, lastErr)
			}
			continue
		}
		var workers []WorkerInfo
		for _, candidate := range observation.candidates {
			if candidate.usable() {
				workers = append(workers, candidate.worker)
			}
			if len(workers) == count {
				break
			}
		}
		if len(workers) == count {
			r.mu.Lock()
			r.workers, r.mayExist = slices.Clone(workers), true
			r.expires = time.Now().Add(r.options.LeaseDuration)
			r.mu.Unlock()
			createCtx, createCancel := context.WithTimeout(waitCtx, reservationOperationTimeout)
			createErr := backend.create(createCtx, r.id, WorkerNames(workers), r.options.LeaseDuration)
			createCancel()
			// A fresh targeted read reconciles a lost response. Never retry an uncertain
			// create under a new name, even when an immediate read reports absence.
			observed, checkErr := r.waitForConfirmation(waitCtx)
			if errors.Is(checkErr, errReservationAbsent) {
				checkErr = nil
			}
			if checkErr != nil || observed == nil {
				definiteRejection := reservationContention(createErr) || definiteReservationRejection(createErr)
				r.mu.Lock()
				r.uncertainCreate = !definiteRejection
				if definiteRejection && checkErr == nil {
					r.mayExist, r.workers = false, nil
				}
				r.mu.Unlock()
				if checkErr != nil {
					return nil, fmt.Errorf("confirm reservation %s: create=%v: %w", r.id, createErr, checkErr)
				}
				if !reservationContention(createErr) {
					return nil, fmt.Errorf("confirm reservation %s: reservation is absent; create=%v", r.id, createErr)
				}
				lastErr = createErr
				r.manager.discovery.invalidate()
			} else {
				if err := r.validateObserved(observed); err != nil {
					return nil, err
				}
				r.manager.invalidate()
				// Retain the whole fixed set if readiness changes after creation. There is
				// no partial grant and no replacement with a different logical worker.
				for {
					if err := waitCtx.Err(); err != nil {
						return nil, err
					}
					if time.Until(r.expires) <= r.options.JobDrainTimeout+r.options.RecoveryTimeout+reservationOperationTimeout {
						return nil, fmt.Errorf("reserve workers: reservation %s is too near expiry", r.id)
					}
					refreshed, err := verify(waitCtx, workers)
					if err == nil {
						fresh, checkErr := backend.get(waitCtx, r.id)
						if checkErr != nil {
							err = checkErr
						} else {
							if checkErr := r.validateObserved(fresh); checkErr != nil {
								return nil, checkErr
							}
							r.mu.Lock()
							if r.state != reservationAcquiring {
								r.mu.Unlock()
								return nil, fmt.Errorf("reserve workers: scope is closing")
							}
							r.workers, r.state = slices.Clone(refreshed), reservationOwned
							r.mu.Unlock()
							return refreshed, nil
						}
					}
					if !isPendingRecovery(err) && !transientDiscoveryError(err) {
						return nil, err
					}
					if waitErr := waitReservation(waitCtx, nil); waitErr != nil {
						return nil, fmt.Errorf("verify acquired workers: %w; last observation: %v", waitErr, err)
					}
				}
			}
		} else {
			var pool []WorkerInfo
			var problems []string
			for _, candidate := range observation.candidates {
				pool = append(pool, candidate.worker)
				if !candidate.usable() {
					problems = append(problems, fmt.Sprintf("%s state=%s reason=%s", candidate.worker.Name, candidate.worker.SlurmNode.State, candidate.reason))
				}
			}
			lastErr = fmt.Errorf("%s; unavailable=%v", WorkerAvailability(pool, observation.configured, count, "matching workers", WorkerInfo.IsUsable), problems)
		}
		if err := waitReservation(waitCtx, changed); err != nil {
			return nil, fmt.Errorf("reserve workers for %q: %w; last observation: %v", r.name, err, lastErr)
		}
	}
}

func (r *workerReservation) waitForConfirmation(ctx context.Context) (*slurmReservation, error) {
	deadline := r.expires.Add(-r.options.JobDrainTimeout - r.options.RecoveryTimeout - reservationOperationTimeout)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		observed, err := r.backend.get(ctx, r.id)
		if !transientDiscoveryError(err) {
			return observed, err
		}
		if waitErr := waitReservation(ctx, nil); waitErr != nil {
			return nil, fmt.Errorf("confirm reservation %s: %w; last observation: %v", r.id, waitErr, err)
		}
	}
}

func waitReservation(ctx context.Context, changed <-chan struct{}) error {
	timer := time.NewTimer(reservationPollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-changed:
		return nil
	case <-timer.C:
		return nil
	}
}

func definiteReservationRejection(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "access denied") || strings.Contains(text, "permission denied") || strings.Contains(text, "invalid user") || strings.Contains(text, "invalid reservation") || strings.Contains(text, "invalid node")
}

func (r *workerReservation) validateMembership(observed *slurmReservation) error {
	if observed == nil {
		return fmt.Errorf("verify reservation %s: reservation is absent", r.id)
	}
	r.mu.Lock()
	want := WorkerNames(r.workers)
	r.mu.Unlock()
	got := slices.Clone(observed.Nodes)
	slices.Sort(want)
	slices.Sort(got)
	if observed.Name != r.id || !slices.Equal(want, got) {
		return fmt.Errorf("verify reservation %s: unexpected membership: %+v", r.id, observed)
	}
	return nil
}

func (r *workerReservation) validateOwnership(observed *slurmReservation) error {
	if !slices.Contains(observed.Flags, "STATIC") || (observed.State != "" && observed.State != "ACTIVE") {
		return fmt.Errorf("verify reservation %s: unexpected flags or state: %+v", r.id, observed)
	}
	for _, flag := range []string{"REPLACE", "REPLACE_DOWN", "OVERLAP", "MAINT", "IGNORE_JOBS", "FLEX", "ANY_NODES", "MAGNETIC", "PURGE_COMP", "NO_HOLD_JOBS_AFTER", "NO_HOLD_JOBS_AFTER_END"} {
		if slices.Contains(observed.Flags, flag) {
			return fmt.Errorf("verify reservation %s: unsafe flag %s", r.id, flag)
		}
	}
	return nil
}

func (r *workerReservation) validateObserved(observed *slurmReservation) error {
	if err := r.validateMembership(observed); err != nil {
		return err
	}
	if err := r.validateOwnership(observed); err != nil {
		return err
	}
	lifetime, err := reservationDurationValue(observed.Duration)
	if err != nil || lifetime < r.options.LeaseDuration || !slices.Contains(observed.Users, "root") {
		return fmt.Errorf("verify reservation %s: unexpected duration=%s users=%v: %v", r.id, observed.Duration, observed.Users, err)
	}
	return nil
}

// WorkerReservationName returns the native lease name after successful acquisition.
func WorkerReservationName(ctx context.Context) (string, error) {
	r := workerReservationFromContext(ctx)
	if r == nil {
		return "", nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != reservationOwned || time.Until(r.expires) <= 0 {
		return "", fmt.Errorf("use reservation: acquire workers before starting work")
	}
	return r.id, nil
}

func newReservationID() string { return "e2e-" + strings.ToLower(rand.Text()[:24]) }
