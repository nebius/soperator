package soperatorchecks

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

// poweredDownWorkerGracePeriod is how long a slurmd may run on a powered down node before its pod is
// recreated. It covers a fresh slurmd whose registration has not reached the listed node state yet,
// and limits the recreation rate when registration keeps failing.
const poweredDownWorkerGracePeriod = 5 * time.Minute

// recreatePoweredDownWorkers deletes worker pods that still run slurmd while Slurm considers their
// node powered down, so that the StatefulSet starts a fresh slurmd.
//
// Slurm accepts a manual power down even for nodes in SuspendExcNodes, and the worker pod keeps
// running. slurmctld does not ping powered down nodes, so the old slurmd never registers again, and
// only a registration brings the node back. Slurm sets POWERED_DOWN once SuspendTimeout has passed,
// and from then on it accepts a slurmd that has just started.
func (c *SlurmNodesController) recreatePoweredDownWorkers(
	ctx context.Context,
	slurmNodes map[types.NamespacedName][]slurmapi.Node,
) error {
	var errs []error
	for slurmClusterName, nodes := range slurmNodes {
		for _, node := range nodes {
			if !needsFreshSlurmd(node) {
				continue
			}
			if err := c.recreatePoweredDownWorker(ctx, slurmClusterName, node.Name); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// needsFreshSlurmd skips nodes in a power transition or a reboot: those are still handled by Slurm
// or by the rolling update.
func needsFreshSlurmd(node slurmapi.Node) bool {
	return node.IsPoweredDownState() &&
		!node.IsPoweringDownState() &&
		!node.IsPoweringUpState() &&
		!node.IsRebootRequestedState() &&
		!node.IsRebootIssuedState()
}

func (c *SlurmNodesController) recreatePoweredDownWorker(
	ctx context.Context,
	slurmClusterName types.NamespacedName,
	slurmNodeName string,
) error {
	pod := &corev1.Pod{}
	podKey := client.ObjectKey{Namespace: slurmClusterName.Namespace, Name: slurmNodeName}
	if err := c.Get(ctx, podKey, pod); err != nil {
		// Suspended ephemeral nodes have no pod.
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get worker pod %s: %w", podKey, err)
	}

	if pod.Labels[consts.LabelWorkerKey] != consts.LabelWorkerValue || pod.DeletionTimestamp != nil {
		return nil
	}
	startedAt, running := slurmdStartedAt(pod)
	if !running || time.Since(startedAt) < poweredDownWorkerGracePeriod {
		return nil
	}

	log.FromContext(ctx).WithName("SlurmNodesController.recreatePoweredDownWorker").Info(
		"Deleting worker pod of powered down slurm node",
		"pod", podKey,
		"slurmdStartedAt", startedAt,
	)
	// The pod comes from the cache: the UID precondition protects a replacement not observed yet.
	err := c.Delete(ctx, pod, client.Preconditions{UID: &pod.UID})
	if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
		return fmt.Errorf("delete worker pod %s: %w", podKey, err)
	}
	return nil
}

func slurmdStartedAt(pod *corev1.Pod) (startedAt time.Time, running bool) {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == consts.ContainerNameSlurmd && status.State.Running != nil {
			return status.State.Running.StartedAt.Time, true
		}
	}
	return time.Time{}, false
}
