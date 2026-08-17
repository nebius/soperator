package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/soperator-e2e/acceptance/internal/kubeobjects"
)

const SoperatorNamespace = "soperator"

type KubectlClient struct {
	exec Exec
}

type NodeSetInfo struct {
	Name          string
	Namespace     string
	Replicas      int
	ReadyReplicas int
	HasGPU        bool
	Phase         string
}

type WorkerPodInfo struct {
	SlurmNodeName      string
	PodName            string
	KubernetesNodeName string
	Ready              bool
}

func NewKubectlClient(exec Exec) *KubectlClient {
	return &KubectlClient{exec: exec}
}

func (c *KubectlClient) GetJSON(ctx context.Context, out any, args ...string) error {
	output, err := c.exec.Kubectl().RunWithDefaultRetry(ctx, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(output), out); err != nil {
		return fmt.Errorf("decode kubectl %s output: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (c *KubectlClient) NodeSets(ctx context.Context, clusterName string) ([]NodeSetInfo, error) {
	var nodeSets kubeobjects.NodeSetList
	if err := c.GetJSON(ctx, &nodeSets, "get", "nodesets", "-n", SoperatorNamespace, "-o", "json"); err != nil {
		return nil, fmt.Errorf("list NodeSets: %w", err)
	}

	out := make([]NodeSetInfo, 0, len(nodeSets.Items))
	for _, nodeSet := range nodeSets.Items {
		// Some legacy Soperator NodeSets may leave spec.clusterName empty.
		if clusterName != "" && nodeSet.Spec.ClusterName != "" && nodeSet.Spec.ClusterName != clusterName {
			continue
		}
		out = append(out, NodeSetInfo{
			Name:          nodeSet.Metadata.Name,
			Namespace:     nodeSet.Metadata.Namespace,
			Replicas:      int(nodeSet.Spec.Replicas),
			ReadyReplicas: int(nodeSet.Status.Replicas),
			HasGPU:        nodeSet.Spec.GPU.Enabled,
			Phase:         nodeSet.Status.Phase,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace == out[j].Namespace {
			return out[i].Name < out[j].Name
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out, nil
}

func (c *KubectlClient) WorkerPods(ctx context.Context) ([]WorkerPodInfo, error) {
	var pods corev1.PodList
	if err := c.GetJSON(ctx, &pods, "get", "pods", "-n", SoperatorNamespace, "-l", "slurm.nebius.ai/worker=true", "-o", "json"); err != nil {
		return nil, fmt.Errorf("list worker pods: %w", err)
	}

	seen := make(map[string]string, len(pods.Items))
	out := make([]WorkerPodInfo, 0, len(pods.Items))
	for _, pod := range pods.Items {
		slurmNodeName := strings.TrimSpace(pod.Spec.Hostname)
		if slurmNodeName == "" {
			return nil, fmt.Errorf("worker pod %s has empty spec.hostname", pod.Name)
		}
		if existing, ok := seen[slurmNodeName]; ok {
			return nil, fmt.Errorf("worker pods %s and %s both declare spec.hostname=%s", existing, pod.Name, slurmNodeName)
		}
		seen[slurmNodeName] = pod.Name

		out = append(out, WorkerPodInfo{
			SlurmNodeName:      slurmNodeName,
			PodName:            pod.Name,
			KubernetesNodeName: pod.Spec.NodeName,
			Ready:              podReady(pod),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].SlurmNodeName < out[j].SlurmNodeName
	})
	return out, nil
}

func (c *KubectlClient) WorkerPodForSlurmNode(ctx context.Context, slurmNodeName string) (WorkerPodInfo, error) {
	target := strings.TrimSpace(slurmNodeName)
	if target == "" {
		return WorkerPodInfo{}, fmt.Errorf("Slurm worker name is empty")
	}

	pods, err := c.WorkerPods(ctx)
	if err != nil {
		return WorkerPodInfo{}, err
	}
	for _, pod := range pods {
		if pod.SlurmNodeName == target {
			return pod, nil
		}
	}
	return WorkerPodInfo{}, fmt.Errorf("worker pod for Slurm node %q was not found", target)
}

func podReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
