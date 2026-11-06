package framework

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// WorkerPool coordinates cooperative worker ownership within one acceptance run.
// Use one pool for all participating suites targeting the same cluster. It does
// not create Slurm reservations or coordinate separate runner processes.
type WorkerPool struct {
	mu         sync.Mutex
	owners     map[string]*workerReservation
	quarantine map[string]error
	waiting    []*workerRequest
	changed    chan struct{}
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
	pool          *WorkerPool
	name          string
	state         reservationState
	workers       []WorkerInfo
	jobs          map[string]struct{}
	cleanupErr    error
	acquireCancel context.CancelFunc
	acquireDone   chan struct{}
	finishDone    chan struct{}
	finishErr     error
}

type workerCandidate struct {
	worker WorkerInfo
	reason string
}

func (c workerCandidate) usable() bool {
	return c.reason == "" && c.worker.SlurmNode.State != "" && c.worker.IsUsable()
}

type workerRequest struct {
	owner      *workerReservation
	ctx        context.Context
	count      int
	candidates []workerCandidate
	granted    []WorkerInfo
	configured int
}

type workerDiscovery struct {
	candidates []workerCandidate
	configured int
}

// NewWorkerPool creates an empty pool without querying or modifying a cluster.
func NewWorkerPool() *WorkerPool {
	return &WorkerPool{
		owners: make(map[string]*workerReservation), quarantine: make(map[string]error), changed: make(chan struct{}),
	}
}

// notifyLocked wakes waiters; callers must hold p.mu.
func (p *WorkerPool) notifyLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

// grantLocked considers the oldest fitting request, allowing backfill without
// holding partial sets. Grants remain tentative until the recipient revalidates
// them against a fresh observation outside the pool lock.
func (p *WorkerPool) grantLocked() {
	for _, request := range p.waiting {
		if len(request.granted) != 0 || request.ctx.Err() != nil || request.owner.state != reservationAcquiring {
			continue
		}
		var available []WorkerInfo
		for _, candidate := range request.candidates {
			name := candidate.worker.Name
			if candidate.usable() && p.owners[name] == nil && p.quarantine[name] == nil {
				available = append(available, candidate.worker)
			}
		}
		if len(available) < request.count {
			continue
		}
		request.granted = slices.Clone(available[:request.count])
		request.owner.workers = request.granted
		for _, worker := range request.granted {
			p.owners[worker.Name] = request.owner
		}
		p.notifyLocked()
	}
}

func (p *WorkerPool) releaseLocked(r *workerReservation, quarantineErr error) {
	for _, worker := range r.workers {
		if p.owners[worker.Name] != r {
			continue
		}
		if quarantineErr != nil {
			p.quarantine[worker.Name] = quarantineErr
		}
		delete(p.owners, worker.Name)
	}
	r.workers = nil
	p.grantLocked()
	p.notifyLocked()
}

func (r *workerReservation) acquire(ctx context.Context, count int, discover func(context.Context) (workerDiscovery, error)) ([]WorkerInfo, error) {
	waitCtx, cancel := context.WithTimeout(ctx, WorkerAcquireTimeout(ctx))
	defer cancel()
	p := r.pool
	request := &workerRequest{owner: r, ctx: waitCtx, count: count}
	p.mu.Lock()
	if r.state != reservationNew {
		p.mu.Unlock()
		return nil, fmt.Errorf("reserve workers for %q: acquire the complete set once per scope", r.name)
	}
	r.state = reservationAcquiring
	r.acquireCancel = cancel
	r.acquireDone = make(chan struct{})
	p.waiting = append(p.waiting, request)
	p.mu.Unlock()
	success := false
	defer func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.waiting = slices.DeleteFunc(p.waiting, func(other *workerRequest) bool { return other == request })
		if !success {
			p.releaseLocked(r, nil)
			if r.state == reservationAcquiring {
				r.state = reservationNew
			}
		}
		r.acquireCancel = nil
		close(r.acquireDone)
	}()

	var lastErr error
	for {
		if err := waitCtx.Err(); err != nil {
			return nil, fmt.Errorf("reserve workers for %q: %w; last observation: %v", r.name, err, lastErr)
		}
		observation, err := discover(waitCtx)
		if IsInsufficientWorkers(err) {
			return nil, err
		}
		p.mu.Lock()
		if r.state != reservationAcquiring {
			p.mu.Unlock()
			return nil, fmt.Errorf("reserve workers for %q: scope is finalizing: %w", r.name, context.Canceled)
		}
		if err != nil {
			lastErr = err
			request.candidates = nil
			request.granted = nil
			p.releaseLocked(r, nil)
		} else {
			candidates := observation.candidates
			request.candidates = candidates
			request.configured = observation.configured
			if len(request.granted) != 0 {
				if workers := revalidateGrant(request.granted, candidates); len(workers) != 0 && waitCtx.Err() == nil {
					r.workers = workers
					r.state = reservationOwned
					p.mu.Unlock()
					success = true
					return slices.Clone(workers), nil
				}
				request.granted = nil
				p.releaseLocked(r, nil)
			}
			lastErr = p.requestObservationLocked(request)
			if p.quarantinePreventsRequestLocked(request) {
				p.mu.Unlock()
				return nil, fmt.Errorf("reserve workers for %q: %w", r.name, lastErr)
			}
			p.grantLocked()
			if len(request.granted) != 0 {
				p.mu.Unlock()
				continue
			}
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-waitCtx.Done():
		case <-changed:
		case <-time.After(DefaultPollInterval):
		}
	}
}

func revalidateGrant(granted []WorkerInfo, candidates []workerCandidate) []WorkerInfo {
	var workers []WorkerInfo
	for _, selected := range granted {
		index := slices.IndexFunc(candidates, func(candidate workerCandidate) bool {
			return candidate.worker.Name == selected.Name && candidate.usable()
		})
		if index == -1 {
			return nil
		}
		workers = append(workers, candidates[index].worker)
	}
	return workers
}

func (p *WorkerPool) quarantinePreventsRequestLocked(request *workerRequest) bool {
	quarantined := 0
	for _, candidate := range request.candidates {
		if p.quarantine[candidate.worker.Name] != nil {
			quarantined++
		}
	}
	return quarantined > 0 && request.configured-quarantined < request.count
}

func (p *WorkerPool) requestObservationLocked(request *workerRequest) error {
	var observations []string
	for _, candidate := range request.candidates {
		worker := candidate.worker
		owner := ""
		if held := p.owners[worker.Name]; held != nil {
			owner = held.name
		}
		observations = append(observations, fmt.Sprintf("%s state=%s reason=%q unavailable=%q owner=%q quarantined=%v",
			worker.Name, worker.SlurmNode.State, worker.SlurmNode.Reason, candidate.reason, owner, p.quarantine[worker.Name]))
	}
	return fmt.Errorf("need %d workers (%d configured, %d discovered): %s", request.count, request.configured, len(request.candidates), strings.Join(observations, "; "))
}
