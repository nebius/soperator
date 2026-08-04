package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/soperator-e2e/acceptance/framework"
	"nebius.ai/soperator-e2e/acceptance/internal/kubeobjects"
)

const SoperatorNamespace = "soperator"

// DiscoverCluster discovers Soperator worker inventory and writes it into state.
func DiscoverCluster(ctx context.Context, exec framework.Exec, state *framework.ClusterState) error {
	if _, err := exec.Kubectl().RunWithDefaultRetry(ctx, "get", "pods", "-n", SoperatorNamespace); err != nil {
		return err
	}
	if err := verifyPodReady(ctx, exec, SoperatorNamespace, state.PodName("login-0")); err != nil {
		return fmt.Errorf("verify login pod: %w", err)
	}
	if err := verifyPodReady(ctx, exec, SoperatorNamespace, state.PodName("controller-0")); err != nil {
		return fmt.Errorf("verify controller pod: %w", err)
	}
	if _, err := exec.Controller().RunWithDefaultRetry(ctx, "true"); err != nil {
		return fmt.Errorf("exec controller sanity check: %w", err)
	}
	if _, err := exec.Jail().RunWithDefaultRetry(ctx, "true"); err != nil {
		return fmt.Errorf("exec login jail sanity check: %w", err)
	}

	discoveredNodeSets, err := discoverNodeSets(ctx, exec, state.SlurmClusterName)
	if err != nil {
		return fmt.Errorf("discover NodeSets: %w", err)
	}
	state.DiscoveredNodeSets = discoveredNodeSets
	log.Printf("acceptance: discovered nodesets: %s", DiscoveredNodeSetSummary(state.DiscoveredNodeSets))

	workerOutput, err := exec.Controller().RunWithDefaultRetry(ctx, `sinfo -hN -p main -o '%N'`)
	if err != nil {
		return fmt.Errorf("discover worker nodes: %w", err)
	}
	workerPods, err := discoverWorkerPods(ctx, exec)
	if err != nil {
		return fmt.Errorf("discover worker pods: %w", err)
	}

	seen := make(map[string]struct{})
	var workers []framework.WorkerRef
	for _, line := range strings.Split(workerOutput, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		podName, ok := workerPods[name]
		if !ok {
			return fmt.Errorf("worker pod for Slurm node %q was not discovered", name)
		}
		seen[name] = struct{}{}
		workers = append(workers, framework.WorkerRef{Name: name, PodName: podName})
	}
	if len(workers) == 0 {
		return fmt.Errorf("no worker nodes discovered")
	}
	state.Workers = workers
	ClassifyWorkers(state)
	if err := VerifyDiscoveredWorkers(state); err != nil {
		return err
	}

	log.Printf("acceptance: discovered workers: %s", WorkerNames(state.Workers))
	log.Printf("acceptance: discovered CPU workers: %s", WorkerNames(state.CPUWorkers))
	log.Printf("acceptance: discovered GPU workers: %s", WorkerNames(state.GPUWorkers))
	log.Printf("acceptance: discovered workers by nodeset: %s", WorkersByNodeSetSummary(state.WorkersByNodeSet))
	return nil
}

func discoverNodeSets(ctx context.Context, exec framework.Exec, clusterName string) ([]framework.DiscoveredNodeSet, error) {
	output, err := exec.Kubectl().RunWithDefaultRetry(ctx, "get", "nodesets", "-n", SoperatorNamespace, "-o", "json")
	if err != nil {
		return nil, err
	}

	var nodeSets kubeobjects.NodeSetList
	if err := json.Unmarshal([]byte(output), &nodeSets); err != nil {
		return nil, fmt.Errorf("decode NodeSet list: %w", err)
	}

	discovered := DiscoveredNodeSetsFromLiveList(nodeSets, clusterName)
	if len(discovered) == 0 {
		return nil, fmt.Errorf("no NodeSets found in namespace %s for Slurm cluster %q", SoperatorNamespace, clusterName)
	}
	return discovered, nil
}

func discoverWorkerPods(ctx context.Context, exec framework.Exec) (map[string]string, error) {
	output, err := exec.Kubectl().RunWithDefaultRetry(ctx,
		"get", "pods", "-n", SoperatorNamespace, "-l", "slurm.nebius.ai/worker=true", "-o", "json")
	if err != nil {
		return nil, err
	}

	var pods corev1.PodList
	if err := json.Unmarshal([]byte(output), &pods); err != nil {
		return nil, fmt.Errorf("decode worker pod list: %w", err)
	}
	return WorkerPodsBySlurmNodeName(pods)
}

func WorkerPodsBySlurmNodeName(pods corev1.PodList) (map[string]string, error) {
	workers := make(map[string]string, len(pods.Items))
	for _, pod := range pods.Items {
		nodeName := strings.TrimSpace(pod.Spec.Hostname)
		if nodeName == "" {
			return nil, fmt.Errorf("worker pod %s has empty spec.hostname", pod.Name)
		}
		if existing, ok := workers[nodeName]; ok {
			return nil, fmt.Errorf("worker pods %s and %s both declare spec.hostname=%s", existing, pod.Name, nodeName)
		}
		workers[nodeName] = pod.Name
	}
	if len(workers) == 0 {
		return nil, fmt.Errorf("no worker pods found")
	}
	return workers, nil
}

func DiscoveredNodeSetsFromLiveList(nodeSets kubeobjects.NodeSetList, clusterName string) []framework.DiscoveredNodeSet {
	discovered := make([]framework.DiscoveredNodeSet, 0, len(nodeSets.Items))
	for _, nodeSet := range nodeSets.Items {
		if clusterName != "" && nodeSet.Spec.ClusterName != "" && nodeSet.Spec.ClusterName != clusterName {
			continue
		}
		discovered = append(discovered, framework.DiscoveredNodeSet{
			Name:   nodeSet.Metadata.Name,
			Size:   int(nodeSet.Spec.Replicas),
			HasGPU: nodeSet.Spec.GPU.Enabled,
		})
	}

	sort.Slice(discovered, func(i, j int) bool {
		return discovered[i].Name < discovered[j].Name
	})
	return discovered
}

func ClassifyWorkers(state *framework.ClusterState) {
	state.WorkersByNodeSet = make(map[string][]framework.WorkerRef, len(state.DiscoveredNodeSets))
	state.CPUWorkers = nil
	state.GPUWorkers = nil

	if len(state.DiscoveredNodeSets) == 0 {
		return
	}

	discovered := slices.Clone(state.DiscoveredNodeSets)
	sort.Slice(discovered, func(i, j int) bool {
		return len(discovered[i].Name) > len(discovered[j].Name)
	})

	gpuByName := make(map[string]bool, len(discovered))
	for _, nodeSet := range discovered {
		gpuByName[nodeSet.Name] = nodeSet.HasGPU
	}

	for _, worker := range state.Workers {
		for _, nodeSet := range discovered {
			prefix := nodeSet.Name + "-"
			if !strings.HasPrefix(worker.Name, prefix) {
				continue
			}
			state.WorkersByNodeSet[nodeSet.Name] = append(state.WorkersByNodeSet[nodeSet.Name], worker)
			if gpuByName[nodeSet.Name] {
				state.GPUWorkers = append(state.GPUWorkers, worker)
			} else {
				state.CPUWorkers = append(state.CPUWorkers, worker)
			}
			break
		}
	}
}

func VerifyDiscoveredWorkers(state *framework.ClusterState) error {
	var problems []string
	for _, nodeSet := range state.DiscoveredNodeSets {
		liveWorkers := len(state.WorkersByNodeSet[nodeSet.Name])
		if liveWorkers != nodeSet.Size {
			problems = append(problems, fmt.Sprintf("NodeSet %s live workers in Slurm=%d desired=%d", nodeSet.Name, liveWorkers, nodeSet.Size))
		}
	}

	desiredWorkers := state.DesiredWorkerCount()
	if desiredWorkers > 0 && len(state.Workers) != desiredWorkers {
		problems = append(problems, fmt.Sprintf("discovered workers=%d desired=%d", len(state.Workers), desiredWorkers))
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func verifyPodReady(ctx context.Context, exec framework.Exec, namespace, name string) error {
	output, err := exec.Kubectl().RunWithDefaultRetry(ctx, "get", "pod", "-n", namespace, name, "-o", "json")
	if err != nil {
		return err
	}

	var pod corev1.Pod
	if err := json.Unmarshal([]byte(output), &pod); err != nil {
		return fmt.Errorf("decode pod %s/%s: %w", namespace, name, err)
	}
	if pod.Status.Phase != corev1.PodRunning {
		return fmt.Errorf("pod %s/%s phase=%s, want %s", namespace, name, pod.Status.Phase, corev1.PodRunning)
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return nil
		}
	}
	return fmt.Errorf("pod %s/%s is not Ready", namespace, name)
}

func WorkerNames(workers []framework.WorkerRef) string {
	if len(workers) == 0 {
		return "<none>"
	}
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		names = append(names, worker.Name)
	}
	return strings.Join(names, ", ")
}

func WorkersByNodeSetSummary(workersByNodeSet map[string][]framework.WorkerRef) string {
	if len(workersByNodeSet) == 0 {
		return "<none>"
	}

	names := make([]string, 0, len(workersByNodeSet))
	for nodeSet := range workersByNodeSet {
		names = append(names, nodeSet)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, nodeSet := range names {
		parts = append(parts, fmt.Sprintf("%s=[%s]", nodeSet, WorkerNames(workersByNodeSet[nodeSet])))
	}

	return strings.Join(parts, "; ")
}

func DiscoveredNodeSetSummary(nodeSets []framework.DiscoveredNodeSet) string {
	if len(nodeSets) == 0 {
		return "<none>"
	}
	parts := make([]string, 0, len(nodeSets))
	for _, nodeSet := range nodeSets {
		nodeType := "cpu"
		if nodeSet.HasGPU {
			nodeType = "gpu"
		}
		parts = append(parts, fmt.Sprintf("%s=%d/%s", nodeSet.Name, nodeSet.Size, nodeType))
	}
	return strings.Join(parts, ", ")
}
