package sharedsteps

import (
	"context"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func (s *NodeReplacement) theNodeIsDrainingWithANodeProblemReasonWhileTheTestJobIsRunning(ctx context.Context) error {
	err := s.waitForHealthCheckDrain(ctx, s.assertTestJobRunningOnOriginalWorker)
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.maintenanceJob, err)
}

func (s *NodeReplacement) aMaintenanceEventIsTriggeredWhileThatNodeIsDraining(ctx context.Context) error {
	node, err := s.runningTestJobNode(ctx)
	if err == nil && (!node.HasStateFlag("DRAIN") || !healthCheckDrainReasonMatches(node.Reason)) {
		err = fmt.Errorf("worker %s is not draining after the simulated health check: state=%s reason=%q instance=%s",
			node.Name, node.State, node.Reason, node.InstanceID)
	}
	if err == nil {
		err = s.aMaintenanceEventIsTriggeredForThatNode(ctx)
	}
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.maintenanceJob, err)
}

func (s *NodeReplacement) theNodeRemainsDrainedWhileTheTestJobIsRunning(ctx context.Context) error {
	node, err := s.runningTestJobNode(ctx)
	if err == nil && !node.HasStateFlag("DRAIN") {
		err = fmt.Errorf("worker %s is no longer drained: state=%s reason=%q instance=%s", node.Name, node.State, node.Reason, node.InstanceID)
	}
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.maintenanceJob, err)
}

// Let maintenance take over an injected health drain before releasing the allocation.
func (s *NodeReplacement) ensureMaintenanceForCleanup(ctx context.Context) error {
	host, present, err := s.kubectl.NodeIfPresent(ctx, s.original.NodeName)
	if err != nil {
		return err
	}
	if !present || host.UID != s.original.NodeUID || host.DeletionTimestamp != nil {
		return nil
	}
	// maintenanceRequested records an attempted patch, including a rejected one.
	if !nodeMaintenanceEventOwned(host) {
		if err := s.aMaintenanceEventIsTriggeredForThatNode(ctx); err != nil {
			return err
		}
	}
	return s.waitForMaintenanceDrain(ctx, nil)
}

func (s *NodeReplacement) aMaintenanceEventIsTriggeredAfterThatNodeHasFullyDrained(ctx context.Context) error {
	if s.maintenanceJob.IsZero() {
		return fmt.Errorf("wait for fully drained worker: test job is not submitted")
	}
	waitCtx, cancel := context.WithTimeout(ctx, nodeReplacementJobTimeout)
	defer cancel()
	if err := s.slurm.WaitForJobGone(waitCtx, s.maintenanceJob.ID, nodeReplacementJobTimeout); err != nil {
		return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.maintenanceJob, err)
	}
	err := s.waitForReplacementCondition(waitCtx, "original worker fully drained before maintenance", nodeReplacementJobTimeout, time.Second,
		func(waitCtx context.Context) (bool, error) {
			node, err := s.originalWorkerNode(waitCtx)
			if err != nil {
				return false, err
			}
			if !nodeReplacementFullyDrained(node) {
				return false, fmt.Errorf("worker %s is not fully drained: state=%s reason=%q instance=%s CPUAlloc=%s AllocMem=%s",
					node.Name, node.State, node.Reason, node.InstanceID, nodeReplacementAllocation(node.CPUAlloc), nodeReplacementAllocation(node.AllocMemoryMiB))
			}
			// Apply the event in the same observation that proves full drain: replacement can start next.
			if err := s.aMaintenanceEventIsTriggeredForThatNode(waitCtx); err != nil {
				return false, rolloutInvariant("trigger maintenance on fully drained worker %s: %w", node.Name, err)
			}
			return true, nil
		})
	return framework.AnnotateWithJobLog(ctx, s.runtime, s.slurm, s.maintenanceJob, err)
}

func nodeReplacementFullyDrained(node framework.SlurmNodeInfo) bool {
	return node.HasStateFlag("IDLE") && node.HasStateFlag("DRAIN") && !node.HasStateFlag("COMPLETING") &&
		node.CPUAlloc != nil && *node.CPUAlloc == 0 && node.AllocMemoryMiB != nil && *node.AllocMemoryMiB == 0
}

func nodeReplacementAllocation(value *uint64) string {
	if value == nil {
		return "unknown"
	}
	return strconv.FormatUint(*value, 10)
}

func (s *NodeReplacement) assertTestJobRunningOnOriginalWorker(ctx context.Context) error {
	_, err := s.runningTestJobNode(ctx)
	return err
}

func (s *NodeReplacement) runningTestJobNode(ctx context.Context) (framework.SlurmNodeInfo, error) {
	job, err := s.slurm.JobInfo(ctx, s.maintenanceJob.ID)
	if err != nil {
		return framework.SlurmNodeInfo{}, err
	}
	if job.QueueState != "RUNNING" || !job.HasNoRestarts() {
		restarts := "unknown"
		if job.Restarts != nil {
			restarts = strconv.Itoa(*job.Restarts)
		}
		return framework.SlurmNodeInfo{}, rolloutInvariant("test job %s is not running without restarts: state=%q restarts=%s accounting=%q",
			job.ID, job.QueueState, restarts, job.SacctDump)
	}
	node, err := s.originalWorkerNode(ctx)
	if err != nil {
		return node, err
	}
	if (!node.HasStateFlag("ALLOCATED") && !node.HasStateFlag("MIXED")) || node.HasStateFlag("COMPLETING") {
		return node, rolloutInvariant("worker %s stopped running the test job before cancellation: state=%s reason=%q instance=%s",
			node.Name, node.State, node.Reason, node.InstanceID)
	}
	return node, nil
}

func (s *NodeReplacement) originalWorkerNode(ctx context.Context) (framework.SlurmNodeInfo, error) {
	host, present, err := s.kubectl.NodeIfPresent(ctx, s.original.NodeName)
	if err != nil {
		return framework.SlurmNodeInfo{}, err
	}
	if !present || host.UID != s.original.NodeUID || host.DeletionTimestamp != nil {
		return framework.SlurmNodeInfo{}, rolloutInvariant("worker %s original host %s/%s disappeared or started replacement: present=%t UID=%s deleting=%t instance=%s",
			s.original.WorkerName, s.original.NodeName, s.original.NodeUID, present, host.UID, host.DeletionTimestamp != nil, s.original.InstanceID)
	}
	var pod corev1.Pod
	if err := s.kubectl.GetJSON(ctx, &pod, "get", "pod", s.original.PodName, "-n", s.original.Namespace, "-o", "json"); err != nil {
		return framework.SlurmNodeInfo{}, err
	}
	if pod.UID != s.original.PodUID || pod.Spec.NodeName != s.original.NodeName || pod.DeletionTimestamp != nil {
		return framework.SlurmNodeInfo{}, rolloutInvariant("worker %s original pod %s/%s changed before maintenance: expected UID=%s host=%s, observed UID=%s host=%s deleting=%t",
			s.original.WorkerName, s.original.Namespace, s.original.PodName, s.original.PodUID, s.original.NodeName,
			pod.UID, pod.Spec.NodeName, pod.DeletionTimestamp != nil)
	}
	node, err := s.slurm.NodeInfoOnce(ctx, s.original.WorkerName)
	if err != nil {
		return node, err
	}
	if node.InstanceID != s.original.InstanceID {
		return node, rolloutInvariant("worker %s changed Compute instance before maintenance: expected=%s observed=%s state=%s reason=%q",
			node.Name, s.original.InstanceID, node.InstanceID, node.State, node.Reason)
	}
	return node, nil
}
