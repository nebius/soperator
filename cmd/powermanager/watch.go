package main

import (
	"context"
	"fmt"
	"reflect"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

// watchActiveNodes preserves the standalone wait commands' membership-only contract.
func watchActiveNodes(ctx context.Context, client ctrlclient.WithWatch, namespace, name string, ordinals []int32, added bool) error {
	selector := fields.OneTermEqualSelector("metadata.name", name).String()
	lw := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = selector
			list := &slurmv1alpha1.NodeSetPowerStateList{}
			err := client.List(ctx, list, &ctrlclient.ListOptions{Namespace: namespace, Raw: &options})
			return list, err
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = selector
			return client.Watch(ctx, &slurmv1alpha1.NodeSetPowerStateList{}, &ctrlclient.ListOptions{Namespace: namespace, Raw: &options})
		},
	}
	_, err := watchtools.UntilWithSync(ctx, lw, &slurmv1alpha1.NodeSetPowerState{}, nil, func(event watch.Event) (bool, error) {
		state, ok := event.Object.(*slurmv1alpha1.NodeSetPowerState)
		if !ok || event.Type == watch.Deleted || state.DeletionTimestamp != nil {
			return false, nil
		}
		return ordinalsMatch(state, ordinals, added), nil
	})
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return fmt.Errorf("wait for activeNodes on %s/%s (added=%t): %w", namespace, name, added, err)
	}
	return nil
}

func waitForPowerStates(ctx context.Context, client ctrlclient.WithWatch, namespace string, nodes map[string][]int32, targets map[string]*slurmv1alpha1.NodeSetPowerState, added bool) error {
	group, ctx := errgroup.WithContext(ctx)
	for name, target := range targets {
		group.Go(func() error {
			if !ordinalsMatch(target, nodes[name], added) {
				return fmt.Errorf("power action for %s was superseded", name)
			}
			return watchPowerState(ctx, client, namespace, name, target, nodes[name], added)
		})
	}
	return group.Wait()
}

func watchPowerState(ctx context.Context, client ctrlclient.WithWatch, namespace, name string, target *slurmv1alpha1.NodeSetPowerState, ordinals []int32, added bool) error {
	selector := fields.OneTermEqualSelector("metadata.name", name).String()
	lw := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = selector
			list := &slurmv1alpha1.NodeSetList{}
			err := client.List(ctx, list, &ctrlclient.ListOptions{Namespace: namespace, Raw: &options})
			return list, err
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = selector
			return client.Watch(ctx, &slurmv1alpha1.NodeSetList{}, &ctrlclient.ListOptions{Namespace: namespace, Raw: &options})
		},
	}
	lastObservation := "No PowerStateReady condition observed"
	var checked *slurmv1alpha1.AppliedPowerState
	var checkedNodeSetGeneration int64
	_, err := watchtools.UntilWithSync(ctx, lw, &slurmv1alpha1.NodeSet{}, nil, func(event watch.Event) (bool, error) {
		nodeSet, ok := event.Object.(*slurmv1alpha1.NodeSet)
		if !ok {
			return false, nil
		}
		if event.Type == watch.Deleted || nodeSet.DeletionTimestamp != nil {
			return false, fmt.Errorf("NodeSet %s was deleted", name)
		}
		if nodeSet.Spec.EphemeralNodes == nil || !*nodeSet.Spec.EphemeralNodes {
			return false, fmt.Errorf("ephemeral mode disabled for %s", name)
		}
		if condition := meta.FindStatusCondition(nodeSet.Status.Conditions, slurmv1alpha1.ConditionNodeSetPowerReady); condition != nil {
			lastObservation = fmt.Sprintf("PowerStateReady=%s, reason=%s, message=%s", condition.Status, condition.Reason, condition.Message)
		}
		applied := nodeSet.Status.AppliedPowerState
		if applied == nil || applied.UID != target.UID || applied.Generation < target.Generation {
			return false, nil
		}
		if !powerStateAcknowledged(nodeSet, target) {
			return false, nil
		}
		if checkedNodeSetGeneration == nodeSet.Generation && reflect.DeepEqual(checked, applied) {
			return false, nil
		}
		// Only a ready observation needs a live read to detect a superseding action.
		// Rechecking the same snapshot cannot change which ordinals it acknowledges.
		current := &slurmv1alpha1.NodeSetPowerState{}
		if err := client.Get(ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: name}, current); err != nil {
			return false, err
		}
		if current.UID != target.UID {
			return false, fmt.Errorf("power state for %s was replaced", name)
		}
		if !ordinalsMatch(current, ordinals, added) {
			return false, fmt.Errorf("power action for %s was superseded", name)
		}
		checked = applied.DeepCopy()
		checkedNodeSetGeneration = nodeSet.Generation
		if applied.ActiveNodes != nil {
			return activeNodesMatch(applied.ActiveNodes, ordinals, added), nil
		}
		// Older operators do not publish a membership snapshot. The action's original
		// state or a matching live generation still provides an unambiguous snapshot.
		if applied.Generation == target.Generation {
			return ordinalsMatch(target, ordinals, added), nil
		}
		return applied.Generation == current.Generation, nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("wait for PowerStateReady on %s (%s): %w", name, lastObservation, ctx.Err())
		}
		return fmt.Errorf("watch PowerStateReady on %s: %w", name, err)
	}
	return nil
}

func powerStateAcknowledged(nodeSet *slurmv1alpha1.NodeSet, target *slurmv1alpha1.NodeSetPowerState) bool {
	applied := nodeSet.Status.AppliedPowerState
	condition := meta.FindStatusCondition(nodeSet.Status.Conditions, slurmv1alpha1.ConditionNodeSetPowerReady)
	return applied != nil && applied.UID == target.UID && applied.Generation >= target.Generation &&
		condition != nil && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == nodeSet.Generation
}

func ordinalsMatch(state *slurmv1alpha1.NodeSetPowerState, ordinals []int32, added bool) bool {
	return activeNodesMatch(state.Spec.ActiveNodes, ordinals, added)
}

func activeNodesMatch(activeNodes []int32, ordinals []int32, added bool) bool {
	active := make(map[int32]bool, len(activeNodes))
	for _, ordinal := range activeNodes {
		active[ordinal] = true
	}
	for _, ordinal := range ordinals {
		if active[ordinal] != added {
			return false
		}
	}
	return true
}
