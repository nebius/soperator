package nodesetcontroller

// Static node recreation is a workaround for Slurm's power saving on static nodes.
//
// Static NodeSets are in SuspendExcNodes, but Slurm still accepts a manual power down. It runs
// SuspendProgram and marks the node POWERED_DOWN after SuspendTimeout while the worker pod keeps
// running. Only a slurmd registration brings the node back, but slurmctld does not ping powered down
// nodes, and it ignores a registration from a slurmd that started within SuspendTimeout of the power
// down (waiting_for_node_power_down). So power-manager adds a request to
// NodeSetPowerState.spec.recreateNodes, and the NodeSet controller recreates the worker pod once
// SuspendTimeout has passed.
//
// To remove the workaround:
//   - delete this file and its test, and cmd/powermanager/static_recreate.go and its test;
//   - in reconcile.go, delete the NodeSetPowerState of static NodeSets again instead of calling
//     reconcileStaticNodeSetPowerState (see git history), and drop recreateRequestedPods and its requeue;
//   - drop the recreateStaticNodes call in cmd/powermanager/main.go;
//   - remove NodeSetPowerStateSpec.RecreateNodes and regenerate the CRDs;
//   - remove the "Static Nodes" section of docs/ephemeral-nodes.md.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

const (
	// recreateDeadlineMargin covers the clock difference between slurmctld, which sets RequestedAt,
	// and the operator.
	recreateDeadlineMargin = 10 * time.Second
	recreateRetryInterval  = 5 * time.Second
	// maxRecreateRequestsPerReconcile keeps a reconcile short when many nodes are powered down at once.
	maxRecreateRequestsPerReconcile = 100
)

// reconcileStaticNodeSetPowerState keeps every ordinal of a static NodeSet active, so that a later
// switch to ephemeral mode starts with all workers running.
func (r *NodeSetReconciler) reconcileStaticNodeSetPowerState(
	ctx context.Context,
	nodeSet *slurmv1alpha1.NodeSet,
) error {
	powerState, err := r.reconcileNodeSetPowerState(ctx, nodeSet)
	if err != nil {
		return err
	}

	activeNodes := make([]int32, nodeSet.Spec.Replicas)
	for i := int32(0); i < nodeSet.Spec.Replicas; i++ {
		activeNodes[i] = i
	}
	if slices.Equal(powerState.Spec.ActiveNodes, activeNodes) {
		return nil
	}

	patch := client.MergeFromWithOptions(powerState.DeepCopy(), client.MergeFromWithOptimisticLock{})
	powerState.Spec.ActiveNodes = activeNodes
	if err = r.Patch(ctx, powerState, patch); err != nil {
		return fmt.Errorf("activate all NodeSetPowerState ordinals: %w", err)
	}
	return nil
}

// recreateRequestedPods handles the recreation requests of the NodeSet and returns when the pending
// ones must be checked again.
func (r *NodeSetReconciler) recreateRequestedPods(
	ctx context.Context,
	nodeSet *slurmv1alpha1.NodeSet,
	cluster *slurmv1.SlurmCluster,
) (time.Duration, error) {
	powerState := &slurmv1alpha1.NodeSetPowerState{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(nodeSet), powerState); err != nil {
		if apierrors.IsNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("get NodeSetPowerState: %w", err)
	}
	if len(powerState.Spec.RecreateNodes) == 0 {
		return 0, nil
	}

	suspendTimeout := time.Duration(
		ptr.Deref(cluster.Spec.SlurmConfig.SuspendTimeout, consts.SlurmDefaultSuspendTimeout),
	) * time.Second

	var (
		requeueAfter time.Duration
		processed    int
		errs         []error
	)
	handled := make(map[int32]metav1.Time)
	for _, request := range powerState.Spec.RecreateNodes {
		deadline := request.RequestedAt.Add(suspendTimeout + recreateDeadlineMargin)
		if wait := time.Until(deadline); wait > 0 {
			requeueAfter = earliestRequeue(requeueAfter, wait)
			continue
		}
		if processed >= maxRecreateRequestsPerReconcile {
			requeueAfter = earliestRequeue(requeueAfter, recreateRetryInterval)
			continue
		}
		processed++

		done, err := r.recreateRequestedPod(ctx, nodeSet, request.Ordinal, deadline)
		switch {
		case err != nil:
			errs = append(errs, err)
		case done:
			handled[request.Ordinal] = request.RequestedAt
		default:
			requeueAfter = earliestRequeue(requeueAfter, recreateRetryInterval)
		}
	}

	if len(handled) > 0 {
		if err := r.removeRecreateRequests(ctx, powerState, handled); err != nil {
			errs = append(errs, fmt.Errorf("remove handled recreate requests: %w", err))
		}
	}
	return requeueAfter, errors.Join(errs...)
}

// recreateRequestedPod deletes the worker pod of the node unless its slurmd started after the deadline,
// and reports whether the request is done.
func (r *NodeSetReconciler) recreateRequestedPod(
	ctx context.Context,
	nodeSet *slurmv1alpha1.NodeSet,
	ordinal int32,
	deadline time.Time,
) (bool, error) {
	podName := fmt.Sprintf("%s-%d", naming.BuildNodeSetStatefulSetName(nodeSet.Name), ordinal)
	logger := log.FromContext(ctx).WithValues("pod", podName, "deadline", deadline)

	if ordinal >= nodeSet.Spec.Replicas {
		logger.Info("Dropping recreate request for ordinal out of NodeSet replicas", "replicas", nodeSet.Spec.Replicas)
		return true, nil
	}

	pod := &corev1.Pod{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: nodeSet.Namespace, Name: podName}, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("get worker pod %s: %w", podName, err)
	}

	// Slurm compares the slurmd start time (slurmd runs with -b) with the power down. A slurmd started
	// after the deadline registers after the power down completes, and one not running yet starts later.
	startedAt, running := slurmdStartedAt(pod)
	switch {
	case pod.DeletionTimestamp != nil:
		return true, nil
	case !running || !startedAt.Before(deadline):
		return true, nil
	case pod.Labels[consts.LabelNodeSetKey] != nodeSet.Name:
		logger.Info("Dropping recreate request for pod of another NodeSet", "podNodeSet", pod.Labels[consts.LabelNodeSetKey])
		return true, nil
	}

	logger.Info("Deleting worker pod of powered down node")
	// The UID precondition protects a replacement pod that the cache has not observed yet.
	// A conflict means the cache is stale, so the request is checked again with the replacement pod.
	err := r.Delete(ctx, pod, client.Preconditions{UID: &pod.UID})
	switch {
	case err == nil, apierrors.IsNotFound(err):
		return true, nil
	case apierrors.IsConflict(err):
		return false, nil
	default:
		return false, fmt.Errorf("delete worker pod %s: %w", podName, err)
	}
}

// removeRecreateRequests removes the handled requests and keeps the ones that power-manager added or
// renewed in the meantime.
func (r *NodeSetReconciler) removeRecreateRequests(
	ctx context.Context,
	powerState *slurmv1alpha1.NodeSetPowerState,
	handled map[int32]metav1.Time,
) error {
	key := client.ObjectKeyFromObject(powerState)
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Get(ctx, key, powerState); err != nil {
			return err
		}
		patch := client.MergeFromWithOptions(powerState.DeepCopy(), client.MergeFromWithOptimisticLock{})
		requests := len(powerState.Spec.RecreateNodes)
		powerState.Spec.RecreateNodes = slices.DeleteFunc(
			powerState.Spec.RecreateNodes,
			func(request slurmv1alpha1.NodeRecreateRequest) bool {
				requestedAt, ok := handled[request.Ordinal]
				return ok && request.RequestedAt.Equal(&requestedAt)
			},
		)
		if len(powerState.Spec.RecreateNodes) == requests {
			return nil
		}
		return r.Patch(ctx, powerState, patch)
	})
}

func slurmdStartedAt(pod *corev1.Pod) (time.Time, bool) {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == consts.ContainerNameSlurmd && status.State.Running != nil {
			return status.State.Running.StartedAt.Time, true
		}
	}
	return time.Time{}, false
}

// earliestRequeue returns the earlier of two RequeueAfter durations, where zero means no requeue.
func earliestRequeue(a, b time.Duration) time.Duration {
	if a == 0 || (b != 0 && b < a) {
		return b
	}
	return a
}
