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

const reservationPollInterval = time.Second

// WorkerPool records unsafe recovery within one run. Slurm, rather than this
// object, owns the workers and arbitrates between runners.
type WorkerPool struct {
	mu         sync.Mutex
	quarantine map[string]error
}

type workerChoice struct {
	count      int
	candidates []WorkerInfo
}

type reservationBackend interface {
	reservations(ctx context.Context) (map[string][]string, error)
	createReservation(ctx context.Context, name string, workers []string) error
	deleteReservation(ctx context.Context, name string) error
	reservationJobs(ctx context.Context, name string) ([]string, error)
	cancelReservationJobs(ctx context.Context, name string) error
	addReservationUser(ctx context.Context, name, user string) error
}

// WorkerReservation retains fixed logical workers until scenario recovery is
// confirmed. An unsafe lease remains visible in Slurm until its finite TTL.
type WorkerReservation struct {
	pool       *WorkerPool
	name       string
	id         string
	workers    []WorkerInfo
	backend    reservationBackend
	cleanupErr error
	finished   bool
}

type reservationContextKey struct{}

func NewWorkerPool() *WorkerPool {
	return &WorkerPool{quarantine: make(map[string]error)}
}

// WithWorkerCleanup records cleanup errors without reserving resources. It is
// used by sequential suites whose scheduler assertions require an ordinary cluster.
func WithWorkerCleanup(ctx context.Context) context.Context {
	return context.WithValue(ctx, reservationContextKey{}, &WorkerReservation{})
}

func WithWorkerReservation(ctx context.Context, pool *WorkerPool, name string) context.Context {
	return context.WithValue(ctx, reservationContextKey{}, &WorkerReservation{
		pool: pool, name: name, id: "e2e-" + strings.ToLower(rand.Text()[:20]),
	})
}

func cleanupReservation(ctx context.Context) *WorkerReservation {
	r, _ := ctx.Value(reservationContextKey{}).(*WorkerReservation)
	return r
}

func workerReservation(ctx context.Context) *WorkerReservation {
	if r := cleanupReservation(ctx); r != nil && r.pool != nil {
		return r
	}
	return nil
}

func RecordWorkerCleanupError(ctx context.Context, err error) {
	if r := cleanupReservation(ctx); r != nil {
		r.cleanupErr = errors.Join(r.cleanupErr, err)
	}
}

func (r *WorkerReservation) acquireAny(ctx context.Context, backend reservationBackend, discover func(context.Context) ([]workerChoice, error)) ([]WorkerInfo, error) {
	if r.backend != nil {
		return nil, fmt.Errorf("scenario %q already owns a reservation; acquire its complete worker set together", r.name)
	}
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("reserve workers for %q: %w; last observation: %v", r.name, err, lastErr)
		}
		choices, err := discover(ctx)
		if err != nil {
			return nil, err
		}
		reservations, err := backend.reservations(ctx)
		if err != nil {
			return nil, err
		}
		occupied := make(map[string]bool)
		for _, workers := range reservations {
			for _, worker := range workers {
				occupied[worker] = true
			}
		}
		r.pool.mu.Lock()
		workers := chooseWorkerChoice(choices, func(worker WorkerInfo) bool {
			return !occupied[worker.Name] && r.pool.quarantine[worker.Name] == nil && reservableWorker(worker)
		})
		quarantined := len(choices) > 0
		for _, choice := range choices {
			count := 0
			hasQuarantine := false
			for _, worker := range choice.candidates {
				if r.pool.quarantine[worker.Name] == nil {
					count++
				} else {
					hasQuarantine = true
				}
			}
			quarantined = quarantined && hasQuarantine && len(choice.candidates) >= choice.count && count < choice.count
		}
		r.pool.mu.Unlock()
		if quarantined {
			return nil, fmt.Errorf("reserve workers for %q: all fitting worker sets contain quarantined workers", r.name)
		}
		if len(workers) != 0 {
			// Retain the known ID before the mutation so a lost response can be
			// reconciled and the final hook can clean up this exact lease.
			r.backend, r.workers = backend, workers
			err = backend.createReservation(ctx, r.id, WorkerNames(workers))
			checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			observed, checkErr := backend.reservations(checkCtx)
			cancel()
			if checkErr != nil {
				return nil, errors.Join(err, fmt.Errorf("confirm reservation %s: %w", r.id, checkErr))
			}
			if names, exists := observed[r.id]; exists {
				want := WorkerNames(workers)
				slices.Sort(want)
				slices.Sort(names)
				if !slices.Equal(names, want) {
					return nil, fmt.Errorf("confirm reservation %s: expected workers %v, observed %v", r.id, want, names)
				}
				if err := ctx.Err(); err != nil {
					return nil, fmt.Errorf("reserve workers for %q: %w", r.name, err)
				}
				return workers, nil
			}
			r.backend, r.workers = nil, nil
			if err == nil || !reservationContention(err) {
				return nil, fmt.Errorf("create reservation %s: %w", r.id, errors.Join(err, errors.New("reservation is absent after creation")))
			}
			lastErr = err
		} else {
			lastErr = fmt.Errorf("no idle, healthy complete worker set is free; reservations=%v", reservations)
		}
		select {
		case <-ctx.Done():
		case <-time.After(reservationPollInterval):
		}
	}
}

func reservableWorker(worker WorkerInfo) bool {
	node := worker.SlurmNode
	return worker.IsUsable() && node.HasStateFlag("IDLE") && !node.HasStateFlag("COMPLETING") &&
		!node.HasStateFlag("ALLOCATED") && !node.HasStateFlag("MIXED") && !node.HasStateFlag("RESERVED")
}

// ReleaseReservedWorkers releases a rejected, read-only selection attempt.
// It must never be called after a scenario has mutated its selected workers.
func ReleaseReservedWorkers(ctx context.Context) error {
	r := workerReservation(ctx)
	if r == nil || r.backend == nil {
		return nil
	}
	if err := r.backend.deleteReservation(ctx, r.id); err != nil {
		return err
	}
	r.backend, r.workers = nil, nil
	return nil
}

// FinishWorkerReservation cancels every job in the native lease, including
// submissions whose IDs were lost, before checking recovery and releasing it.
func FinishWorkerReservation(ctx context.Context, runtime Runtime) (result error) {
	r := cleanupReservation(ctx)
	if r == nil || r.finished {
		return nil
	}
	r.finished = true
	result = r.cleanupErr
	if r.backend == nil {
		return result
	}
	defer func() {
		if result != nil {
			r.pool.mu.Lock()
			for _, worker := range r.workers {
				r.pool.quarantine[worker.Name] = result
			}
			r.pool.mu.Unlock()
		}
		runtime.Logf("Finish Slurm reservation id=%s scenario=%q workers=%v recovery_error=%v", r.id, r.name, WorkerNames(r.workers), result)
	}()
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), TargetedWorkerSmokeTimeout+2*time.Minute)
	defer cancel()
	if err := runtime.WaitFor(cleanupCtx, "reservation jobs leave the queue", 2*time.Minute, time.Second,
		func(waitCtx context.Context) (bool, error) {
			jobs, err := r.backend.reservationJobs(waitCtx, r.id)
			if err != nil || len(jobs) == 0 {
				return err == nil, err
			}
			return false, r.backend.cancelReservationJobs(waitCtx, r.id)
		}); err != nil {
		return errors.Join(result, err)
	}
	if err := runtime.WaitFor(cleanupCtx, "reserved workers healthy after cleanup", TargetedWorkerSmokeTimeout, DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			kubectl, slurm := NewKubectlClient(runtime), NewSlurmClient(runtime)
			var hostsReady map[string]bool
			for _, worker := range r.workers {
				node, err := slurm.NodeInfoOnce(waitCtx, worker.Name)
				if err != nil {
					return false, err
				}
				if node.State == "" || !node.IsUsable() || !node.HasStateFlag("IDLE") || node.HasStateFlag("COMPLETING") {
					return false, fmt.Errorf("worker %s state=%s reason=%s", worker.Name, node.State, node.Reason)
				}
				if node.IsReadyForImmediateWork() {
					pod, err := kubectl.WorkerPodForSlurmNode(waitCtx, worker.Name)
					if err != nil {
						return false, err
					}
					if hostsReady == nil {
						hostsReady, err = readyWorkerHosts(waitCtx, kubectl)
						if err != nil {
							return false, err
						}
					}
					if !pod.Ready || !hostsReady[pod.KubernetesNodeName] {
						return false, fmt.Errorf("worker %s pod or Kubernetes host is not Ready", worker.Name)
					}
				}
			}
			return true, nil
		}); err != nil {
		return errors.Join(result, err)
	}
	if result != nil {
		return result
	}
	return r.backend.deleteReservation(cleanupCtx, r.id)
}

// RequireReservedHost prevents a host-wide fault from disrupting workers outside
// the scenario's fixed Slurm reservation.
func RequireReservedHost(ctx context.Context, kubectl *KubectlClient, nodeName string) error {
	r := workerReservation(ctx)
	if r == nil {
		return nil
	}
	pods, err := kubectl.WorkerPods(ctx)
	if err != nil {
		return err
	}
	owned := WorkerNames(r.workers)
	for _, pod := range pods {
		if pod.KubernetesNodeName == nodeName && !slices.Contains(owned, pod.SlurmNodeName) {
			return fmt.Errorf("reserve host %s: worker %s is outside this scenario's reservation", nodeName, pod.SlurmNodeName)
		}
	}
	return nil
}
