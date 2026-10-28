package framework

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// WorkerPool coordinates scenario ownership within one acceptance run.
// Cluster-wide mutations must run in a separate, sequential suite.
type WorkerPool struct {
	mu         sync.Mutex
	owners     map[string]*WorkerReservation
	quarantine map[string]error
	waiting    []*workerRequest
	changed    chan struct{}
}

type workerRequest struct {
	owner      *WorkerReservation
	count      int
	candidates []WorkerInfo
	granted    []WorkerInfo
}

// WorkerReservation retains worker ownership until scenario cleanup finishes.
type WorkerReservation struct {
	pool       *WorkerPool
	name       string
	workers    []WorkerInfo
	jobs       map[string]struct{}
	cleanupErr error
}

type reservationContextKey struct{}

// NewWorkerPool creates an isolated pool; it does not discover or modify a cluster.
func NewWorkerPool() *WorkerPool {
	return &WorkerPool{owners: make(map[string]*WorkerReservation), quarantine: make(map[string]error), changed: make(chan struct{})}
}

// WithWorkerReservation gives a scenario its own ownership and job tracking scope.
func WithWorkerReservation(ctx context.Context, pool *WorkerPool, name string) context.Context {
	return context.WithValue(ctx, reservationContextKey{}, &WorkerReservation{pool: pool, name: name, jobs: make(map[string]struct{})})
}

func workerReservation(ctx context.Context) *WorkerReservation {
	reservation, _ := ctx.Value(reservationContextKey{}).(*WorkerReservation)
	return reservation
}

// RecordWorkerCleanupError preserves cleanup failures without replacing the
// original step error. The final hook reports them and quarantines owned workers.
func RecordWorkerCleanupError(ctx context.Context, err error) {
	if r := workerReservation(ctx); r != nil {
		r.cleanupErr = errors.Join(r.cleanupErr, err)
	}
}

// TrackReservedJob includes jobs launched indirectly, such as by an ActiveCheck,
// in the scenario's final queue check.
func TrackReservedJob(ctx context.Context, id string) {
	if r := workerReservation(ctx); r != nil && id != "" {
		r.jobs[id] = struct{}{}
	}
}

func (p *WorkerPool) notify() {
	close(p.changed)
	p.changed = make(chan struct{})
}

func (p *WorkerPool) grant() {
	var waiting []*workerRequest
	granted := false
	for _, request := range p.waiting {
		var available []WorkerInfo
		for _, worker := range request.candidates {
			if worker.IsUsable() && worker.SlurmNode.State != "" && p.owners[worker.Name] == nil && p.quarantine[worker.Name] == nil {
				available = append(available, worker)
			}
		}
		if len(available) < request.count {
			waiting = append(waiting, request)
			continue
		}
		request.granted = available[:request.count]
		granted = true
		request.owner.workers = request.granted
		for _, worker := range request.granted {
			p.owners[worker.Name] = request.owner
		}
	}
	p.waiting = waiting
	if granted {
		p.notify()
	}
}

func (p *WorkerPool) remove(request *workerRequest) {
	var waiting []*workerRequest
	for _, other := range p.waiting {
		if other != request {
			waiting = append(waiting, other)
		}
	}
	p.waiting = waiting
}

func (r *WorkerReservation) acquire(ctx context.Context, count int, discover func(context.Context) ([]WorkerInfo, error)) ([]WorkerInfo, error) {
	p := r.pool
	request := &workerRequest{owner: r, count: count}
	p.mu.Lock()
	if len(r.workers) != 0 {
		p.mu.Unlock()
		return nil, fmt.Errorf("scenario %q already owns workers; acquire its complete worker set together", r.name)
	}
	p.waiting = append(p.waiting, request)
	p.mu.Unlock()
	success := false
	defer func() {
		p.mu.Lock()
		p.remove(request)
		p.mu.Unlock()
		if !success && len(request.granted) != 0 {
			r.release(nil)
		}
	}()
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("reserve workers for %q: %w; last observation: %v", r.name, err, lastErr)
		}
		candidates, err := discover(ctx)
		if err != nil {
			return nil, err
		}
		// Preserve GPU capacity for requests that require it.
		candidates = append([]WorkerInfo(nil), candidates...)
		sort.SliceStable(candidates, func(i, j int) bool { return !candidates[i].HasGPU && candidates[j].HasGPU })
		p.mu.Lock()
		request.candidates = candidates
		p.grant()
		if len(request.granted) != 0 {
			workers := append([]WorkerInfo(nil), request.granted...)
			p.notify()
			p.mu.Unlock()
			success = true
			return workers, nil
		}
		var observations []string
		quarantined := 0
		for _, worker := range candidates {
			owner := ""
			if held := p.owners[worker.Name]; held != nil {
				owner = held.name
			}
			observations = append(observations, fmt.Sprintf("%s state=%s unavailable=%q owner=%q quarantined=%v", worker.Name, worker.SlurmNode.State, worker.unavailableReason, owner, p.quarantine[worker.Name]))
			if p.quarantine[worker.Name] != nil {
				quarantined++
			}
		}
		lastErr = fmt.Errorf("need %d workers: %s", count, strings.Join(observations, "; "))
		if quarantined > 0 && len(candidates) >= count && len(candidates)-quarantined < count {
			p.mu.Unlock()
			return nil, fmt.Errorf("reserve workers: %w", lastErr)
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		case <-time.After(DefaultPollInterval):
		}
	}
}

// ReleaseReservedWorkers releases a rejected, read-only selection attempt.
// It must never be called after a scenario has mutated its selected workers.
func ReleaseReservedWorkers(ctx context.Context) {
	if r := workerReservation(ctx); r != nil {
		r.release(nil)
	}
}

func (r *WorkerReservation) release(recoveryErr error) {
	p := r.pool
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, worker := range r.workers {
		if recoveryErr != nil {
			p.quarantine[worker.Name] = recoveryErr
		}
		delete(p.owners, worker.Name)
	}
	r.workers = nil
	p.grant()
	p.notify()
}

// FinishWorkerReservation checks owned jobs and worker recovery after all cleanup
// hooks, and quarantines the reservation if its healthy baseline cannot be proved.
func FinishWorkerReservation(ctx context.Context, runtime Runtime) (result error) {
	r := workerReservation(ctx)
	if r == nil {
		return nil
	}
	defer func() {
		if runtime != nil && len(r.workers) != 0 {
			runtime.Logf("Finish worker reservation %s workers=%v recovery_error=%v", r.name, WorkerNames(r.workers), result)
		}
		r.release(result)
	}()
	if r.cleanupErr != nil {
		return r.cleanupErr
	}
	if len(r.workers) == 0 {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), TargetedWorkerSmokeTimeout+2*time.Minute)
	defer cancel()
	slurm := NewSlurmClient(runtime)
	if len(r.jobs) != 0 {
		if err := runtime.WaitFor(cleanupCtx, "reserved jobs leave the queue", 2*time.Minute, DefaultPollInterval,
			func(waitCtx context.Context) (bool, error) {
				output, err := runtime.Jail().Run(waitCtx, "squeue -h -o '%i'")
				if err != nil {
					return false, err
				}
				var active []string
				for _, id := range strings.Fields(output) {
					if _, owned := r.jobs[id]; owned {
						active = append(active, id)
					}
				}
				if len(active) == 0 {
					return true, nil
				}
				for _, id := range active {
					if err := slurm.CancelJob(waitCtx, id, 0); err != nil {
						return false, err
					}
				}
				return false, fmt.Errorf("owned jobs still active: %v", active)
			}); err != nil {
			return err
		}
	}
	return runtime.WaitFor(cleanupCtx, "reserved workers healthy after cleanup", TargetedWorkerSmokeTimeout, DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			kubectl := NewKubectlClient(runtime)
			for _, worker := range r.workers {
				node, err := slurm.NodeInfoOnce(waitCtx, worker.Name)
				if err != nil {
					return false, err
				}
				if node.State == "" || !node.IsUsable() {
					return false, fmt.Errorf("worker %s state=%s reason=%s", worker.Name, node.State, node.Reason)
				}
				if node.IsReadyForImmediateWork() {
					if !node.HasStateFlag("IDLE") {
						return false, fmt.Errorf("worker %s is still occupied: state=%s", worker.Name, node.State)
					}
					pod, err := kubectl.WorkerPodForSlurmNode(waitCtx, worker.Name)
					if err != nil {
						return false, err
					}
					if !pod.Ready {
						return false, fmt.Errorf("worker pod %s is not Ready", pod.PodName)
					}
				}
			}
			return true, nil
		})
}

// RequireReservedHost prevents a host-wide fault from disrupting another
// scenario when multiple Slurm workers share a Kubernetes node.
func RequireReservedHost(ctx context.Context, kubectl *KubectlClient, nodeName string) error {
	r := workerReservation(ctx)
	if r == nil {
		return nil
	}
	pods, err := kubectl.WorkerPods(ctx)
	if err != nil {
		return err
	}
	r.pool.mu.Lock()
	defer r.pool.mu.Unlock()
	for _, pod := range pods {
		if pod.KubernetesNodeName == nodeName && r.pool.owners[pod.SlurmNodeName] != r {
			return fmt.Errorf("reserve host %s: worker %s is outside this scenario's reservation", nodeName, pod.SlurmNodeName)
		}
	}
	return nil
}
