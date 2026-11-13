package framework

import (
	"context"
	"encoding/json"
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
	backend := &slurmReservations{runtime: s.slurm.runtime, hosts: &reservation.manager.hosts}
	workers, err := reservation.acquire(ctx, backend, needs.Count, func(waitCtx context.Context) (workerDiscovery, error) {
		return s.reservationCandidates(waitCtx, needs)
	}, func(verifyCtx context.Context, workers []WorkerInfo) ([]WorkerInfo, error) {
		nodes, err := s.slurm.NodeInfosNoRetries(verifyCtx, WorkerNames(workers))
		if err != nil {
			return nil, err
		}
		if err := recoveredWorkers(verifyCtx, s.slurm.runtime, nodes, workers, needs.Idle); err != nil {
			return nil, err
		}
		for i := range workers {
			workers[i].SlurmNode = nodes[i]
		}
		return workers, nil
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
	r := workerReservationFromContext(ctx)
	observed, err := r.manager.discovery.get(ctx, s.slurmClusterName, reservationPollInterval, func(readCtx context.Context) (reservationDiscovery, error) {
		// Snapshot and readiness apply to all requests. Filter only after sharing the read.
		reader := *s
		kubectl := *s.kubectl
		kubectl.retryTransientOnly = true
		reader.kubectl = &kubectl
		snapshot, err := reader.Snapshot(readCtx)
		if err != nil {
			return reservationDiscovery{}, err
		}
		pods, err := kubectl.WorkerPods(readCtx)
		if err != nil {
			return reservationDiscovery{}, err
		}
		ready, err := workerReadiness(readCtx, &kubectl, pods)
		if err != nil {
			return reservationDiscovery{}, err
		}
		reservations, err := r.backend.list(readCtx)
		if err != nil {
			return reservationDiscovery{}, err
		}
		occupied := make(map[string]string)
		for _, lease := range reservations {
			for _, name := range lease.Nodes {
				occupied[name] = lease.Name
			}
		}
		return reservationDiscovery{snapshot: snapshot, ready: ready, occupied: occupied}, nil
	})
	if err != nil {
		return workerDiscovery{}, err
	}
	snapshot := observed.snapshot
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
	ready := observed.ready
	var candidates []workerCandidate
	for _, worker := range snapshot.Workers {
		if needs.NodeSet != "" && worker.NodeSetName != needs.NodeSet ||
			strings.EqualFold(needs.Kind, "CPU") && worker.HasGPU ||
			strings.EqualFold(needs.Kind, "GPU") && !worker.HasGPU ||
			len(needs.Names) != 0 && !slices.Contains(needs.Names, worker.Name) {
			continue
		}
		candidate := workerCandidate{worker: worker}
		if lease := observed.occupied[worker.Name]; lease != "" {
			candidate.reason = "worker belongs to native reservation " + lease
		}
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

func workerReadiness(ctx context.Context, kubectl *KubectlClient, pods []WorkerPodInfo) (map[string]bool, error) {
	var hosts []string
	for _, pod := range pods {
		if pod.KubernetesNodeName != "" && !slices.Contains(hosts, pod.KubernetesNodeName) {
			hosts = append(hosts, pod.KubernetesNodeName)
		}
	}
	ready := make(map[string]bool, len(pods))
	if len(hosts) == 0 {
		return ready, nil
	}
	slices.Sort(hosts)
	args := append([]string{"get", "nodes"}, hosts...)
	args = append(args, "--ignore-not-found", "-o", "json")
	// kubectl emits a Node for one named host and a List for multiple hosts.
	var response struct {
		corev1.Node
		Items []corev1.Node `json:"items"`
	}
	output, err := kubectl.read(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("read worker Kubernetes hosts: %w", err)
	}
	if strings.TrimSpace(output) == "" {
		return ready, nil
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return nil, fmt.Errorf("decode worker Kubernetes hosts: %w", err)
	}
	if response.Name != "" {
		response.Items = append(response.Items, response.Node)
	}
	hostReady := make(map[string]bool, len(response.Items))
	for _, node := range response.Items {
		hostReady[node.Name] = node.DeletionTimestamp == nil && !node.Spec.Unschedulable && kubeobjects.NodeReady(node)
	}
	for _, pod := range pods {
		ready[pod.SlurmNodeName] = pod.Ready && hostReady[pod.KubernetesNodeName]
	}
	return ready, nil
}
