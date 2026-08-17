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
		if !node.IsUsable() {
			continue
		}
		workers = append(workers, WorkerInfo{Name: node.Name})
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

	snapshot := WorkerSnapshot{
		NodeSets:         nodeSets,
		WorkersByNodeSet: make(map[string][]WorkerInfo, len(nodeSets)),
	}
	classifyWorkers(&snapshot, workers)
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
	workers, err := s.Workers(ctx)
	if err != nil {
		return nil, err
	}
	return pickWorkers(workers, count, "workers")
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
	return pickWorkers(s.Workers, count, "workers")
}

func (s WorkerSnapshot) PickCPUWorkers(count int) ([]WorkerInfo, error) {
	return pickWorkers(s.CPUWorkers, count, "CPU workers")
}

func (s WorkerSnapshot) PickGPUWorkers(count int) ([]WorkerInfo, error) {
	return pickWorkers(s.GPUWorkers, count, "GPU workers")
}

func classifyWorkers(snapshot *WorkerSnapshot, workers []WorkerInfo) {
	nodeSets := append([]NodeSetInfo(nil), snapshot.NodeSets...)
	sort.Slice(nodeSets, func(i, j int) bool {
		return len(nodeSets[i].Name) > len(nodeSets[j].Name)
	})

	for _, worker := range workers {
		classified := worker
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
			break
		}
		snapshot.Workers = append(snapshot.Workers, classified)
	}
}

func pickWorkers(pool []WorkerInfo, count int, label string) ([]WorkerInfo, error) {
	if count < 1 {
		return nil, fmt.Errorf("invalid %s count %d", label, count)
	}
	if len(pool) < count {
		return nil, &InsufficientWorkersError{Label: label, Found: len(pool), Need: count}
	}

	indices := rand.Perm(len(pool))[:count]
	out := make([]WorkerInfo, 0, count)
	for _, i := range indices {
		out = append(out, pool[i])
	}
	return out, nil
}
