package framework

import (
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

// WorkerRequirements describes a complete allocation. Names restrict the
// candidate set; Count workers are chosen from it. Idle requires active, idle
// workers; otherwise healthy powered-down workers remain schedulable.
type WorkerRequirements struct {
	Kind    string
	Count   int
	NodeSet string
	Names   []string
	Docker  bool
	Idle    bool
}

// Acquire waits for and reserves a complete allocation in an explicit ownership
// scope. Snapshot selectors remain read-only and do not establish ownership.
func (s *WorkerSelector) Acquire(ctx context.Context, needs WorkerRequirements) ([]WorkerInfo, error) {
	reservation := workerReservationFromContext(ctx)
	if reservation == nil {
		return nil, fmt.Errorf("reserve workers: an ownership scope is required")
	}
	if needs.Count < 1 {
		return nil, fmt.Errorf("reserve workers: invalid count %d", needs.Count)
	}
	if kind := strings.ToUpper(needs.Kind); kind != "" && kind != "CPU" && kind != "GPU" {
		return nil, fmt.Errorf("reserve workers: unsupported type %q", needs.Kind)
	}
	seen := make(map[string]bool, len(needs.Names))
	for _, name := range needs.Names {
		if !IsSlurmNodeName(name) || seen[name] {
			return nil, fmt.Errorf("reserve workers: invalid or duplicate name %q", name)
		}
		seen[name] = true
	}
	if len(needs.Names) != 0 && len(needs.Names) < needs.Count {
		return nil, fmt.Errorf("reserve workers: %d names cannot satisfy count %d", len(needs.Names), needs.Count)
	}
	needs.Names = slices.Clone(needs.Names)
	workers, err := reservation.acquire(ctx, needs.Count, func(waitCtx context.Context) (workerDiscovery, error) {
		return s.reservationCandidates(waitCtx, needs)
	})
	if err == nil {
		s.slurm.runtime.Logf("Reserved workers %v for %s", WorkerNames(workers), reservation.name)
	}
	return workers, err
}

// Reserve acquires every named worker for a custom selection. Record baselines
// only after it succeeds, using freshly queried state.
func (s *WorkerSelector) Reserve(ctx context.Context, needs WorkerRequirements) error {
	if len(needs.Names) == 0 || len(needs.Names) != needs.Count {
		return fmt.Errorf("reserve selected workers: names must describe the complete allocation")
	}
	_, err := s.Acquire(ctx, needs)
	return err
}

func (s *WorkerSelector) reservationCandidates(ctx context.Context, needs WorkerRequirements) (workerDiscovery, error) {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return workerDiscovery{}, err
	}
	if needs.Docker {
		snapshot = snapshot.WithDocker()
	}
	var configured int
	for _, nodeSet := range snapshot.NodeSets {
		if needs.NodeSet != "" && nodeSet.Name != needs.NodeSet ||
			strings.EqualFold(needs.Kind, "CPU") && nodeSet.HasGPU ||
			strings.EqualFold(needs.Kind, "GPU") && !nodeSet.HasGPU {
			continue
		}
		configured += max(0, nodeSet.Replicas)
	}
	if configured < needs.Count {
		return workerDiscovery{}, &InsufficientWorkersError{Label: "matching workers", Found: configured, Need: needs.Count}
	}
	if len(needs.Names) != 0 {
		configured = min(configured, len(needs.Names))
	}
	ready, err := workerReadiness(ctx, s.kubectl)
	if err != nil {
		return workerDiscovery{}, err
	}
	var candidates []workerCandidate
	for _, worker := range snapshot.Workers {
		if needs.NodeSet != "" && worker.NodeSetName != needs.NodeSet ||
			strings.EqualFold(needs.Kind, "CPU") && worker.HasGPU ||
			strings.EqualFold(needs.Kind, "GPU") && !worker.HasGPU ||
			len(needs.Names) != 0 && !slices.Contains(needs.Names, worker.Name) {
			continue
		}
		candidate := workerCandidate{worker: worker}
		if worker.SlurmNode.IsReadyForImmediateWork() && !ready[worker.Name] {
			candidate.reason = "worker pod or Kubernetes host is not Ready"
		}
		if needs.Idle && (!worker.IsIdle() || worker.SlurmNode.HasStateFlag("COMPLETING")) {
			candidate.reason = "worker is not active and idle"
		}
		candidates = append(candidates, candidate)
	}
	return workerDiscovery{candidates: candidates, configured: configured}, nil
}

func workerReadiness(ctx context.Context, kubectl *KubectlClient) (map[string]bool, error) {
	pods, err := kubectl.WorkerPods(ctx)
	if err != nil {
		return nil, err
	}
	var nodes corev1.NodeList
	if err := kubectl.GetJSON(ctx, &nodes, "get", "nodes", "-o", "json"); err != nil {
		return nil, fmt.Errorf("list worker Kubernetes hosts: %w", err)
	}
	hostReady := make(map[string]bool, len(nodes.Items))
	for _, node := range nodes.Items {
		hostReady[node.Name] = node.DeletionTimestamp == nil && !node.Spec.Unschedulable && kubeobjects.NodeReady(node)
	}
	ready := make(map[string]bool, len(pods))
	for _, pod := range pods {
		ready[pod.SlurmNodeName] = pod.Ready && hostReady[pod.KubernetesNodeName]
	}
	return ready, nil
}
