package sharedsteps

import (
	"context"
	"fmt"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

// selectDockerWorkers preserves immediate selection for suites without ownership scopes.
func selectDockerWorkers(ctx context.Context, selector *framework.WorkerSelector, kind string, count int) ([]framework.WorkerInfo, error) {
	if framework.HasWorkerReservation(ctx) {
		return selector.Acquire(ctx, framework.WorkerRequirements{Kind: kind, Count: count, Docker: true})
	}
	snapshot, err := selector.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	snapshot = snapshot.WithDocker()
	if kind == "" {
		return snapshot.PickWorkers(count)
	}
	return snapshot.PickWorkersByType(kind, count)
}

func selectIdleGPUWorkers(ctx context.Context, selector *framework.WorkerSelector, count int, oneNodeSet bool) ([]framework.WorkerInfo, error) {
	if count < 1 {
		return nil, fmt.Errorf("select idle GPU workers: invalid count %d", count)
	}
	if framework.HasWorkerReservation(ctx) && !oneNodeSet {
		return selector.Acquire(ctx, framework.WorkerRequirements{Kind: "GPU", Count: count, Idle: true})
	}
	snapshot, err := selector.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if !framework.HasWorkerReservation(ctx) {
		return pickIdleGPUWorkers(snapshot, count, oneNodeSet)
	}
	nodeSet, err := gpuNodeSetWithReplicas(snapshot, count)
	if err != nil {
		return nil, err
	}
	return selector.Acquire(ctx, framework.WorkerRequirements{Kind: "GPU", Count: count, NodeSet: nodeSet, Idle: true})
}
