package sharedsteps

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestRolloutSelectionUsesAvailableCPUOrGPUWithoutWaitingOnBusyCPU(t *testing.T) {
	for _, busyCPU := range []bool{false, true} {
		cpu := rolloutSelectionTestNodeSet("z-cpu", 2)
		cpu.Spec.GPU.Enabled = false
		steps, runtime := rolloutSelectionTestSteps(rolloutSelectionTestNodeSet("a-gpu", 2), cpu)
		pool := framework.NewWorkerPool()
		holder := framework.WithWorkerReservation(t.Context(), pool, "holder")
		if busyCPU {
			_, err := steps.selector.Acquire(holder, framework.WorkerRequirements{NodeSet: "z-cpu", Count: 1})
			require.NoError(t, err)
			defer func() { require.NoError(t, framework.ReleaseReservedWorkers(holder)) }()
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		ctx = framework.WithWorkerReservation(ctx, pool, "rollout")
		require.NoError(t, steps.selectNodeSet(ctx))
		defer func() { require.NoError(t, framework.ReleaseReservedWorkers(ctx)) }()
		want := "z-cpu"
		if busyCPU {
			want = "a-gpu"
		}
		assert.Equal(t, want, steps.nodeSet.Metadata.Name)
		assert.Len(t, steps.workers, 2)
		assert.Empty(t, runtime.submissions, "selection must remain read-only")
	}
}

func TestRolloutSelectionCanUseTwoIndependentCPUNodeSets(t *testing.T) {
	firstCPU, secondCPU := rolloutSelectionTestNodeSet("cpu-a", 2), rolloutSelectionTestNodeSet("cpu-b", 2)
	firstCPU.Spec.GPU.Enabled, secondCPU.Spec.GPU.Enabled = false, false
	first, runtime := rolloutSelectionTestSteps(rolloutSelectionTestNodeSet("a-gpu", 2), firstCPU, secondCPU)
	second := NewWorkerRollingUpdate(first.info, runtime, first.slurm, first.kubectl, first.selector)
	pool := framework.NewWorkerPool()
	firstCtx := framework.WithWorkerReservation(t.Context(), pool, "first rollout")
	secondCtx := framework.WithWorkerReservation(t.Context(), pool, "second rollout")
	require.NoError(t, first.selectNodeSet(firstCtx))
	defer func() { require.NoError(t, framework.ReleaseReservedWorkers(firstCtx)) }()
	require.NoError(t, second.selectNodeSet(secondCtx))
	defer func() { require.NoError(t, framework.ReleaseReservedWorkers(secondCtx)) }()
	assert.Equal(t, "cpu-a", first.nodeSet.Metadata.Name)
	assert.Equal(t, "cpu-b", second.nodeSet.Metadata.Name)
}

func TestRolloutSelectionRejectsUnavailableCPUHostsAndKeepsGPUFallback(t *testing.T) {
	cpu := rolloutSelectionTestNodeSet("cpu", 2)
	cpu.Spec.GPU.Enabled = false
	steps, runtime := rolloutSelectionTestSteps(cpu, rolloutSelectionTestNodeSet("gpu", 2))
	runtime.nodes[0].Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}
	ctx := framework.WithWorkerReservation(t.Context(), framework.NewWorkerPool(), t.Name())
	require.NoError(t, steps.selectNodeSet(ctx))
	defer func() { require.NoError(t, framework.ReleaseReservedWorkers(ctx)) }()
	assert.Equal(t, "gpu", steps.nodeSet.Metadata.Name)
}

func TestRolloutSelectionWaitsWhenEveryCompleteNodeSetIsBusy(t *testing.T) {
	cpu := rolloutSelectionTestNodeSet("cpu", 2)
	cpu.Spec.GPU.Enabled = false
	steps, _ := rolloutSelectionTestSteps(cpu, rolloutSelectionTestNodeSet("gpu", 2))
	pool := framework.NewWorkerPool()
	holder := framework.WithWorkerReservation(t.Context(), pool, "holder")
	_, err := steps.selector.Acquire(holder, framework.WorkerRequirements{Names: []string{"cpu-0", "gpu-0"}, Count: 2})
	require.NoError(t, err)
	defer func() { require.NoError(t, framework.ReleaseReservedWorkers(holder)) }()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	ctx = framework.WithWorkerReservation(ctx, pool, t.Name())
	err = steps.selectNodeSet(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Zero(t, steps.nodeSet)
}
