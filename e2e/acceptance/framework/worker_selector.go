package framework

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"github.com/cucumber/godog"
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
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.PickWorkers(count)
}

func (s *WorkerSelector) PickCPUWorkers(ctx context.Context, count int) ([]WorkerInfo, error) {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.PickCPUWorkers(count)
}

func (s *WorkerSelector) PickGPUWorkers(ctx context.Context, count int) ([]WorkerInfo, error) {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.PickGPUWorkers(count)
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
