package framework

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

type WorkerReplacementSnapshot struct {
	Namespace  string
	WorkerName string
	PodName    string
	PodUID     types.UID
	NodeName   string
	NodeUID    types.UID
	InstanceID string
}

// CaptureWorkerReplacement records the identities that maintenance must replace.
func CaptureWorkerReplacement(ctx context.Context, kubectl *KubectlClient, slurm *SlurmClient, workerName string) (WorkerReplacementSnapshot, error) {
	info, err := kubectl.WorkerPodForSlurmNode(ctx, workerName)
	if err != nil {
		return WorkerReplacementSnapshot{}, err
	}
	if err := RequireReservedHost(ctx, kubectl, info.KubernetesNodeName); err != nil {
		return WorkerReplacementSnapshot{}, err
	}
	var pod corev1.Pod
	if err := kubectl.GetJSON(ctx, &pod, "get", "pod", info.PodName, "-n", SoperatorNamespace, "-o", "json"); err != nil {
		return WorkerReplacementSnapshot{}, fmt.Errorf("read original worker pod %s: %w", info.PodName, err)
	}
	if pod.Name != info.PodName || pod.Namespace != SoperatorNamespace || pod.UID == "" || pod.Spec.NodeName == "" ||
		pod.Spec.Hostname != workerName || pod.DeletionTimestamp != nil || !kubeobjects.PodReady(pod) {
		return WorkerReplacementSnapshot{}, fmt.Errorf("record Ready worker %s: pod=%s/%s UID=%s host=%s hostname=%s ready=%t deleting=%t",
			workerName, pod.Namespace, pod.Name, pod.UID, pod.Spec.NodeName, pod.Spec.Hostname, kubeobjects.PodReady(pod), pod.DeletionTimestamp != nil)
	}
	host, present, err := kubectl.NodeIfPresent(ctx, pod.Spec.NodeName)
	if err != nil {
		return WorkerReplacementSnapshot{}, err
	}
	if !present || host.DeletionTimestamp != nil {
		return WorkerReplacementSnapshot{}, fmt.Errorf("record worker %s host %s: observed name=%s UID=%s deleting=%t",
			workerName, pod.Spec.NodeName, host.Name, host.UID, host.DeletionTimestamp != nil)
	}
	node, err := slurm.NodeInfoOnce(ctx, workerName)
	if err != nil {
		return WorkerReplacementSnapshot{}, err
	}
	if node.InstanceID == "" {
		return WorkerReplacementSnapshot{}, fmt.Errorf("record Compute instance for worker %s: state=%s reason=%s", workerName, node.State, node.Reason)
	}
	return WorkerReplacementSnapshot{
		Namespace: pod.Namespace, WorkerName: workerName, PodName: pod.Name, PodUID: pod.UID,
		NodeName: host.Name, NodeUID: host.UID, InstanceID: node.InstanceID,
	}, nil
}

// ObserveWorkerReplacement verifies Kubernetes, Slurm and Compute replacement.
// The returned Pod lets callers also verify their own workload revision.
func ObserveWorkerReplacement(ctx context.Context, runtime Runtime, kubectl *KubectlClient, slurm *SlurmClient, original WorkerReplacementSnapshot) (corev1.Pod, bool, error) {
	if original.Namespace == "" || original.WorkerName == "" || original.PodName == "" || original.PodUID == "" ||
		original.NodeName == "" || original.NodeUID == "" || original.InstanceID == "" {
		return corev1.Pod{}, false, fmt.Errorf("observe worker replacement: original identities are incomplete")
	}
	var pod corev1.Pod
	if err := kubectl.GetJSON(ctx, &pod, "get", "pod", original.PodName, "-n", original.Namespace, "-o", "json"); err != nil {
		return pod, false, fmt.Errorf("read replacement worker pod %s/%s: %w", original.Namespace, original.PodName, err)
	}
	if pod.Name != original.PodName || pod.Namespace != original.Namespace || pod.Spec.Hostname != original.WorkerName {
		return pod, false, fmt.Errorf("replacement worker %s identity changed: pod=%s/%s hostname=%s",
			original.WorkerName, pod.Namespace, pod.Name, pod.Spec.Hostname)
	}
	if pod.UID == "" || pod.UID == original.PodUID || pod.Spec.NodeName == "" || pod.Spec.NodeName == original.NodeName ||
		pod.DeletionTimestamp != nil || !kubeobjects.PodReady(pod) {
		return pod, false, nil
	}
	host, present, err := kubectl.NodeIfPresent(ctx, original.NodeName)
	if err != nil {
		return pod, false, err
	}
	if present && host.UID == original.NodeUID {
		return pod, false, nil
	}
	node, err := slurm.NodeInfoOnce(ctx, original.WorkerName)
	if err != nil {
		return pod, false, err
	}
	if !node.IsUsableReplacement(original.InstanceID) {
		return pod, false, nil
	}
	removed, err := InstanceRemoved(ctx, runtime, original.InstanceID)
	if err != nil {
		return pod, false, fmt.Errorf("check original Compute instance %s: %w", original.InstanceID, err)
	}
	return pod, removed, nil
}

// WaitForInstanceRemoval waits until Compute no longer returns the original instance.
func WaitForInstanceRemoval(ctx context.Context, runtime Runtime, instanceID string, timeout, pollInterval time.Duration) error {
	return runtime.WaitFor(ctx, "old instance removal", timeout, pollInterval, func(waitCtx context.Context) (bool, error) {
		return InstanceRemoved(waitCtx, runtime, instanceID)
	})
}

// InstanceRemoved checks whether Compute no longer returns an original instance.
// Authentication, network, and other query errors must never count as removal.
func InstanceRemoved(ctx context.Context, runtime Runtime, instanceID string) (bool, error) {
	_, err := runtime.Local().Run(ctx, "nebius", "compute", "instance", "get", "--id", instanceID, "--format", "json")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// WaitForUsableReplacement waits for a Slurm worker to become usable on a different instance.
func WaitForUsableReplacement(ctx context.Context, runtime Runtime, slurm *SlurmClient, workerName, originalInstanceID string, timeout, pollInterval time.Duration) error {
	return runtime.WaitFor(ctx, "replacement node ready", timeout, pollInterval, func(waitCtx context.Context) (bool, error) {
		node, err := slurm.NodeInfo(waitCtx, workerName)
		if err != nil {
			return false, err
		}
		return node.IsUsableReplacement(originalInstanceID), nil
	})
}
