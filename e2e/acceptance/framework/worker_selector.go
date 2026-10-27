package framework

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

type WorkerSelector struct {
	kubectl          *KubectlClient
	slurm            *SlurmClient
	slurmClusterName string
}

type WorkerSnapshot struct {
	Workers          []WorkerInfo
	CPUWorkers       []WorkerInfo
	GPUWorkers       []WorkerInfo
	NodeSets         []NodeSetInfo
	WorkersByNodeSet map[string][]WorkerInfo
}

type InsufficientWorkersError struct {
	Label string
	Found int
	Need  int
}

func NewWorkerSelector(kubectl *KubectlClient, slurm *SlurmClient, slurmClusterName string) *WorkerSelector {
	return &WorkerSelector{
		kubectl:          kubectl,
		slurm:            slurm,
		slurmClusterName: slurmClusterName,
	}
}

func (e *InsufficientWorkersError) Error() string {
	return fmt.Sprintf("found %d %s, need %d", e.Found, e.Label, e.Need)
}

func IsInsufficientWorkers(err error) bool {
	var target *InsufficientWorkersError
	return errors.As(err, &target)
}

func SkipIfInsufficientWorkers(logger Logger, err error) error {
	if err == nil {
		return nil
	}
	if !IsInsufficientWorkers(err) {
		return err
	}
	if logger != nil {
		logger.Logf("acceptance: %v, skipping scenario", err)
	}
	return godog.ErrSkip
}

func (s *WorkerSelector) Workers(ctx context.Context) ([]WorkerInfo, error) {
	names, err := s.slurm.MainPartitionNodeNames(ctx)
	if err != nil {
		return nil, err
	}

	workers := make([]WorkerInfo, 0, len(names))
	if workerReservation(ctx) != nil {
		nodes, err := s.slurm.NodeInfosOnce(ctx, names)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			workers = append(workers, WorkerInfo{Name: node.Name, SlurmNode: node})
		}
		return workers, nil
	}
	for _, name := range names {
		node, err := s.slurm.NodeInfo(ctx, name)
		if err != nil {
			return nil, err
		}
		workers = append(workers, WorkerInfo{Name: node.Name, SlurmNode: node})
	}
	sort.Slice(workers, func(i, j int) bool {
		return workers[i].Name < workers[j].Name
	})
	return workers, nil
}

func (s *WorkerSelector) Snapshot(ctx context.Context) (WorkerSnapshot, error) {
	nodeSets, err := s.kubectl.NodeSets(ctx, s.slurmClusterName)
	if err != nil {
		return WorkerSnapshot{}, err
	}
	if len(nodeSets) == 0 {
		return WorkerSnapshot{}, fmt.Errorf("no NodeSets found in namespace %s for Slurm cluster %q", SoperatorNamespace, s.slurmClusterName)
	}

	workers, err := s.Workers(ctx)
	if err != nil {
		return WorkerSnapshot{}, err
	}

	return newWorkerSnapshot(nodeSets, workers)
}

func newWorkerSnapshot(nodeSets []NodeSetInfo, workers []WorkerInfo) (WorkerSnapshot, error) {
	snapshot := WorkerSnapshot{
		NodeSets:         nodeSets,
		WorkersByNodeSet: make(map[string][]WorkerInfo, len(nodeSets)),
	}
	for _, nodeSet := range nodeSets {
		snapshot.WorkersByNodeSet[nodeSet.Name] = nil
	}
	if err := classifyWorkers(&snapshot, workers); err != nil {
		return WorkerSnapshot{}, err
	}
	return snapshot, nil
}

func (s *WorkerSelector) CPUWorkers(ctx context.Context) ([]WorkerInfo, error) {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.CPUWorkers, nil
}

func (s *WorkerSelector) GPUWorkers(ctx context.Context) ([]WorkerInfo, error) {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.GPUWorkers, nil
}

func (s *WorkerSelector) PickWorkers(ctx context.Context, count int) ([]WorkerInfo, error) {
	return s.Acquire(ctx, WorkerRequirements{Count: count})
}

func (s *WorkerSelector) PickCPUWorkers(ctx context.Context, count int) ([]WorkerInfo, error) {
	return s.Acquire(ctx, WorkerRequirements{Kind: "CPU", Count: count})
}

func (s *WorkerSelector) PickGPUWorkers(ctx context.Context, count int) ([]WorkerInfo, error) {
	return s.Acquire(ctx, WorkerRequirements{Kind: "GPU", Count: count})
}

// WorkerRequirements describes a complete scenario allocation. Names and NodeSet
// restrict selection without changing live discovery or cluster health assertions.
type WorkerRequirements struct {
	Kind    string
	Count   int
	NodeSet string
	Names   []string
	Docker  bool
	Idle    bool
}

// Acquire reserves a complete worker set when called from a runner scenario.
// Outside a reservation scope it retains the existing immediate-selection behavior.
func (s *WorkerSelector) Acquire(ctx context.Context, needs WorkerRequirements) ([]WorkerInfo, error) {
	if needs.Count < 1 {
		return nil, fmt.Errorf("invalid worker count %d", needs.Count)
	}
	if kind := strings.ToUpper(needs.Kind); kind != "" && kind != "CPU" && kind != "GPU" {
		return nil, fmt.Errorf("unsupported worker type %q", needs.Kind)
	}
	seen := make(map[string]bool, len(needs.Names))
	for _, name := range needs.Names {
		if !IsSlurmNodeName(name) || seen[name] {
			return nil, fmt.Errorf("reserve workers: invalid or duplicate worker name %q", name)
		}
		seen[name] = true
	}
	discover := func(ctx context.Context) ([]WorkerInfo, error) {
		snapshot, err := s.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		if needs.Docker {
			snapshot = snapshot.WithDocker()
		}
		var configured int
		for _, nodeSet := range snapshot.NodeSets {
			if needs.NodeSet != "" && nodeSet.Name != needs.NodeSet ||
				strings.EqualFold(needs.Kind, "GPU") && !nodeSet.HasGPU ||
				strings.EqualFold(needs.Kind, "CPU") && nodeSet.HasGPU {
				continue
			}
			if nodeSet.Replicas > 0 {
				configured += nodeSet.Replicas
			}
		}
		if len(needs.Names) != 0 {
			configured = min(configured, len(needs.Names))
		}
		if configured < needs.Count {
			return nil, &InsufficientWorkersError{Label: "matching workers", Found: configured, Need: needs.Count}
		}
		var candidates []WorkerInfo
		ready := make(map[string]bool)
		if workerReservation(ctx) != nil {
			pods, err := s.kubectl.WorkerPods(ctx)
			if err != nil {
				return nil, err
			}
			var nodes corev1.NodeList
			if err := s.kubectl.GetJSON(ctx, &nodes, "get", "nodes", "-o", "json"); err != nil {
				return nil, fmt.Errorf("list worker Kubernetes hosts: %w", err)
			}
			hostReady := make(map[string]bool, len(nodes.Items))
			for _, node := range nodes.Items {
				hostReady[node.Name] = kubeobjects.NodeReady(node)
			}
			for _, pod := range pods {
				ready[pod.SlurmNodeName] = pod.Ready && hostReady[pod.KubernetesNodeName]
			}
		}
		for _, worker := range snapshot.Workers {
			if needs.NodeSet != "" && worker.NodeSetName != needs.NodeSet ||
				strings.EqualFold(needs.Kind, "GPU") && !worker.HasGPU ||
				strings.EqualFold(needs.Kind, "CPU") && worker.HasGPU ||
				len(needs.Names) != 0 && !slices.Contains(needs.Names, worker.Name) ||
				needs.Idle && (!worker.SlurmNode.IsReadyForImmediateWork() || !worker.SlurmNode.HasStateFlag("IDLE")) {
				continue
			}
			if workerReservation(ctx) != nil && worker.SlurmNode.IsReadyForImmediateWork() && !ready[worker.Name] {
				worker.unavailableReason = "worker pod or Kubernetes host is not Ready"
			}
			candidates = append(candidates, worker)
		}
		return candidates, nil
	}
	if reservation := workerReservation(ctx); reservation != nil {
		started := time.Now()
		s.slurm.runtime.Logf("Waiting for %d %s workers for %s", needs.Count, needs.Kind, reservation.name)
		waitCtx, cancel := context.WithTimeout(ctx, WorkerAcquireTimeout(ctx))
		defer cancel()
		workers, err := reservation.acquire(waitCtx, needs.Count, discover)
		if err == nil {
			s.slurm.runtime.Logf("Reserved workers %v for %s after %s", WorkerNames(workers), reservation.name, time.Since(started).Round(time.Millisecond))
		}
		return workers, err
	}
	candidates, err := discover(ctx)
	if err != nil {
		return nil, err
	}
	return pickWorkers(candidates, needs.Count, needs.Count, "matching workers")
}

type workerAcquireTimeoutKey struct{}

// Reserve acquires a known set for custom selectors before recording baselines.
// Direct callers outside runner scenarios keep their existing selection behavior.
func (s *WorkerSelector) Reserve(ctx context.Context, needs WorkerRequirements) error {
	if workerReservation(ctx) == nil {
		return nil
	}
	_, err := s.Acquire(ctx, needs)
	return err
}

// WithWorkerAcquireTimeout bounds lease and recovery waits independently of job timeouts.
func WithWorkerAcquireTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, workerAcquireTimeoutKey{}, timeout)
}

func WorkerAcquireTimeout(ctx context.Context) time.Duration {
	if timeout, ok := ctx.Value(workerAcquireTimeoutKey{}).(time.Duration); ok && timeout > 0 {
		return timeout
	}
	return 90 * time.Minute
}

func (s WorkerSnapshot) PickWorkers(count int) ([]WorkerInfo, error) {
	return pickWorkers(s.Workers, desiredWorkerCount(s.NodeSets), count, "workers")
}

func (s WorkerSnapshot) PickCPUWorkers(count int) ([]WorkerInfo, error) {
	return pickWorkers(s.CPUWorkers, desiredCPUWorkerCount(s.NodeSets), count, "CPU workers")
}

func (s WorkerSnapshot) PickGPUWorkers(count int) ([]WorkerInfo, error) {
	return pickWorkers(s.GPUWorkers, desiredGPUWorkerCount(s.NodeSets), count, "GPU workers")
}

// PickWorkersByType selects CPU or GPU workers, accepting either type case-insensitively.
func (s WorkerSnapshot) PickWorkersByType(workerType string, count int) ([]WorkerInfo, error) {
	switch strings.ToUpper(workerType) {
	case "CPU":
		return s.PickCPUWorkers(count)
	case "GPU":
		return s.PickGPUWorkers(count)
	default:
		return nil, fmt.Errorf("unsupported worker type %q", workerType)
	}
}

// WithDocker returns a snapshot containing only Docker-enabled NodeSets and their workers.
func (s WorkerSnapshot) WithDocker() WorkerSnapshot {
	filtered := WorkerSnapshot{WorkersByNodeSet: make(map[string][]WorkerInfo)}
	for _, nodeSet := range s.NodeSets {
		if !nodeSet.DockerEnabled {
			continue
		}
		filtered.NodeSets = append(filtered.NodeSets, nodeSet)
		workers := s.WorkersByNodeSet[nodeSet.Name]
		filtered.WorkersByNodeSet[nodeSet.Name] = slices.Clone(workers)
		filtered.Workers = append(filtered.Workers, workers...)
		if nodeSet.HasGPU {
			filtered.GPUWorkers = append(filtered.GPUWorkers, workers...)
		} else {
			filtered.CPUWorkers = append(filtered.CPUWorkers, workers...)
		}
	}
	return filtered
}

func (s WorkerSnapshot) RequireAllWorkersUsable() error {
	return requireAllWorkersUsable(s.Workers, desiredWorkerCount(s.NodeSets), "workers")
}

func (s WorkerSnapshot) RequireGPUWorkersUsable() error {
	desired := desiredGPUWorkerCount(s.NodeSets)
	if desired == 0 && len(s.GPUWorkers) == 0 {
		return &InsufficientWorkersError{Label: "GPU workers", Found: 0, Need: 1}
	}
	return requireAllWorkersUsable(s.GPUWorkers, desired, "GPU workers")
}

func classifyWorkers(snapshot *WorkerSnapshot, workers []WorkerInfo) error {
	nodeSets := append([]NodeSetInfo(nil), snapshot.NodeSets...)
	sort.Slice(nodeSets, func(i, j int) bool {
		return len(nodeSets[i].Name) > len(nodeSets[j].Name)
	})

	var unmatched []string
	for _, worker := range workers {
		classified := worker
		matched := false
		for _, nodeSet := range nodeSets {
			prefix := nodeSet.Name + "-"
			if !strings.HasPrefix(worker.Name, prefix) {
				continue
			}
			classified.NodeSetName = nodeSet.Name
			classified.HasGPU = nodeSet.HasGPU
			snapshot.WorkersByNodeSet[nodeSet.Name] = append(snapshot.WorkersByNodeSet[nodeSet.Name], classified)
			if nodeSet.HasGPU {
				snapshot.GPUWorkers = append(snapshot.GPUWorkers, classified)
			} else {
				snapshot.CPUWorkers = append(snapshot.CPUWorkers, classified)
			}
			matched = true
			break
		}
		if !matched {
			unmatched = append(unmatched, worker.Name)
		}
		snapshot.Workers = append(snapshot.Workers, classified)
	}
	if len(unmatched) > 0 {
		sort.Strings(unmatched)
		known := make([]string, 0, len(snapshot.NodeSets))
		for _, nodeSet := range snapshot.NodeSets {
			known = append(known, nodeSet.Name)
		}
		sort.Strings(known)
		return fmt.Errorf("workers do not match any NodeSet: %s; known NodeSets: %s",
			strings.Join(unmatched, ", "), strings.Join(known, ", "))
	}
	return nil
}

func pickWorkers(pool []WorkerInfo, configured, count int, label string) ([]WorkerInfo, error) {
	if count < 1 {
		return nil, fmt.Errorf("invalid %s count %d", label, count)
	}
	if configured < count {
		return nil, &InsufficientWorkersError{Label: label, Found: configured, Need: count}
	}

	usable := usableWorkers(pool)
	if len(usable) < count {
		return nil, unavailableWorkersError(pool, configured, count, label)
	}

	indices := rand.Perm(len(usable))[:count]
	out := make([]WorkerInfo, 0, count)
	for _, i := range indices {
		out = append(out, usable[i])
	}
	return out, nil
}

func requireAllWorkersUsable(pool []WorkerInfo, configured int, label string) error {
	if configured == len(pool) && len(usableWorkers(pool)) == configured {
		return nil
	}
	return unavailableWorkersError(pool, configured, configured, label)
}

func usableWorkers(workers []WorkerInfo) []WorkerInfo {
	usable := make([]WorkerInfo, 0, len(workers))
	for _, worker := range workers {
		if worker.IsUsable() {
			usable = append(usable, worker)
		}
	}
	return usable
}

func desiredWorkerCount(nodeSets []NodeSetInfo) int {
	var count int
	for _, nodeSet := range nodeSets {
		if nodeSet.Replicas > 0 {
			count += nodeSet.Replicas
		}
	}
	return count
}

func desiredCPUWorkerCount(nodeSets []NodeSetInfo) int {
	var count int
	for _, nodeSet := range nodeSets {
		if nodeSet.Replicas > 0 && !nodeSet.HasGPU {
			count += nodeSet.Replicas
		}
	}
	return count
}

func desiredGPUWorkerCount(nodeSets []NodeSetInfo) int {
	var count int
	for _, nodeSet := range nodeSets {
		if nodeSet.Replicas > 0 && nodeSet.HasGPU {
			count += nodeSet.Replicas
		}
	}
	return count
}

func unavailableWorkersError(pool []WorkerInfo, configured, need int, label string) error {
	usable := usableWorkers(pool)
	var problems []string
	for _, worker := range pool {
		if worker.IsUsable() {
			continue
		}
		problems = append(problems, fmt.Sprintf("%s state=%s reason=%s",
			worker.Name, worker.SlurmNode.State, worker.SlurmNode.Reason))
	}
	if missing := configured - len(pool); missing > 0 {
		problems = append(problems, fmt.Sprintf("%d configured %s missing from Slurm", missing, label))
	}
	if extra := len(pool) - configured; extra > 0 {
		problems = append(problems, fmt.Sprintf("%d unexpected %s present in Slurm", extra, label))
	}
	sort.Strings(problems)
	message := fmt.Sprintf("found %d usable %s, need %d (%d configured, %d discovered)",
		len(usable), label, need, configured, len(pool))
	if len(problems) > 0 {
		message += ": " + strings.Join(problems, "; ")
	}
	return errors.New(message)
}
