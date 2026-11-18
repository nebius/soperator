package main

// Static node recreation is a workaround for Slurm's power saving on static nodes.
//
// A manual power down of a static node still runs SuspendProgram, but the worker pod keeps running.
// Slurm only takes a powered down node back when a fresh slurmd registers, so power-manager asks the
// NodeSet controller to recreate the worker pods (see
// internal/controller/nodesetcontroller/static_recreate.go).
//
// Deprecated: part of the static worker feature, will be removed together with it.
//
// To remove the workaround, delete this file and its test, and the staticRequests WaitGroup and the
// startStaticRecreations call in applyPowerAction.

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

var recreateRequestBackoff = wait.Backoff{Steps: 8, Duration: 100 * time.Millisecond, Factor: 2, Jitter: 0.2, Cap: 5 * time.Second}

// startStaticRecreations requests the recreation of the worker pods of every static NodeSet in a
// suspend, one goroutine per NodeSet, so that a NodeSet waiting for its NodeSetPowerState and failed
// ephemeral updates do not stop the others. Failures are only logged: Slurm ignores the exit status
// of power programs.
func startStaticRecreations(
	ctx context.Context,
	requests *sync.WaitGroup,
	client ctrlclient.Client,
	namespace string,
	nodeSets map[string]*slurmv1alpha1.NodeSet,
	nodesByNodeSet map[string][]int32,
) {
	// metav1.Time is serialized with second precision; truncate so retries compare equal.
	requestedAt := metav1.Now().Rfc3339Copy()
	for nodeSetName, ordinals := range nodesByNodeSet {
		nodeSet, exists := nodeSets[nodeSetName]
		if !exists || (nodeSet.Spec.EphemeralNodes != nil && *nodeSet.Spec.EphemeralNodes) {
			continue
		}
		requests.Go(func() {
			if err := requestNodeRecreation(ctx, client, namespace, nodeSetName, ordinals, requestedAt); err != nil {
				log.Error(err, "Failed to request worker pod recreation", "nodeSet", nodeSetName)
				return
			}
			log.Info("Requested worker pod recreation", "nodeSet", nodeSetName, "ordinals", ordinals, "requestedAt", requestedAt)
		})
	}
}

func requestNodeRecreation(ctx context.Context, client ctrlclient.Client, namespace, nodeSetName string, ordinals []int32, requestedAt metav1.Time) error {
	powerStateKey := ctrlclient.ObjectKey{
		Namespace: namespace,
		Name:      nodeSetName,
	}

	missing := false
	attempt := func() error {
		powerState := &slurmv1alpha1.NodeSetPowerState{}
		err := client.Get(ctx, powerStateKey, powerState)
		missing = apierrors.IsNotFound(err)
		if err != nil {
			if ctrlclient.IgnoreNotFound(err) != nil {
				return fmt.Errorf("get NodeSetPowerState: %w", err)
			}
			return err
		}
		if !addRecreateRequests(powerState, ordinals, requestedAt) {
			return nil
		}
		if err := client.Update(ctx, powerState); err != nil {
			return err
		}
		// An outdated CRD prunes spec.recreateNodes, and the update still succeeds.
		if !hasRecreateRequests(powerState, ordinals, requestedAt) {
			return fmt.Errorf("store recreate requests in NodeSetPowerState %s/%s: the API server dropped spec.recreateNodes, upgrade the NodeSetPowerState CRD",
				namespace, nodeSetName)
		}
		return nil
	}
	err := retryPowerStateUpdate(ctx, recreateRequestBackoff, attempt)
	if err != nil && ctx.Err() != nil && missing {
		return fmt.Errorf("wait for operator to create NodeSetPowerState %s/%s: %w", namespace, nodeSetName, ctx.Err())
	}
	return err
}

// hasRecreateRequests reports whether every ordinal has a request made at requestedAt or later.
func hasRecreateRequests(powerState *slurmv1alpha1.NodeSetPowerState, ordinals []int32, requestedAt metav1.Time) bool {
	for _, ordinal := range ordinals {
		if !slices.ContainsFunc(powerState.Spec.RecreateNodes, func(request slurmv1alpha1.NodeRecreateRequest) bool {
			return request.Ordinal == ordinal && !request.RequestedAt.Before(&requestedAt)
		}) {
			return false
		}
	}
	return true
}

// addRecreateRequests reports whether the requests changed.
func addRecreateRequests(powerState *slurmv1alpha1.NodeSetPowerState, ordinals []int32, requestedAt metav1.Time) bool {
	changed := false
	requests := powerState.Spec.RecreateNodes
	for _, ordinal := range ordinals {
		i := slices.IndexFunc(requests, func(request slurmv1alpha1.NodeRecreateRequest) bool {
			return request.Ordinal == ordinal
		})
		if i < 0 {
			requests = append(requests, slurmv1alpha1.NodeRecreateRequest{Ordinal: ordinal, RequestedAt: requestedAt})
			changed = true
			continue
		}
		// A pod recreated for a pending earlier request still predates this power down.
		if requests[i].RequestedAt.Before(&requestedAt) {
			requests[i].RequestedAt = requestedAt
			changed = true
		}
	}
	if changed {
		sort.Slice(requests, func(i, j int) bool {
			return requests[i].Ordinal < requests[j].Ordinal
		})
		powerState.Spec.RecreateNodes = requests
	}
	return changed
}
