package sharedsteps

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type systemChecksTestRuntime struct {
	*concurrentSelectionRuntime
	worker     framework.CommandScope
	controller framework.CommandScope
}

func (r *systemChecksTestRuntime) Controller() framework.CommandScope {
	if r.controller != nil {
		return r.controller
	}
	return r.concurrentSelectionRuntime.Controller()
}

func (r *systemChecksTestRuntime) WorkerPod(framework.WorkerPodInfo) framework.CommandScope {
	return r.worker
}

func (r *systemChecksTestRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		if args[0] == "get" && args[1] == "--raw" {
			return `{"pods":[{"podRef":{"uid":"worker-0"},"ephemeral-storage":{"usedBytes":0}}]}`, nil
		}
		return r.concurrentSelectionRuntime.Kubectl().Run(ctx, args...)
	})
}

func TestWorkerRestartCleanupDoesNotTouchEphemeralStorage(t *testing.T) {
	runtime := &systemChecksTestRuntime{concurrentSelectionRuntime: concurrentSelectionFixture()}
	runtime.worker = framework.NewCommandScope(func(context.Context, string) (string, error) {
		t.Fatal("a worker restart must not remove an unowned ephemeral fill file")
		return "", nil
	})
	kubectl, slurm := framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime)
	selector := framework.NewWorkerSelector(kubectl, slurm, "soperator")
	pool := framework.NewWorkerPool()
	ctx := framework.WithWorkerReservation(t.Context(), pool, "worker restart")
	steps := NewSystemChecks(runtime, slurm, kubectl, selector)
	require.NoError(t, steps.aHealthyWorkerPodIsSelected(ctx))
	worker := steps.worker.Name
	steps.CleanupAndReset(ctx)
	require.NoError(t, framework.FinishWorkerReservation(ctx, runtime))

	next := framework.WithWorkerReservation(t.Context(), pool, "next scenario")
	_, err := selector.Acquire(next, framework.WorkerRequirements{Names: []string{worker}, Count: 1})
	require.NoError(t, err, "unrelated cleanup must not quarantine the restarted worker")
	require.NoError(t, framework.FinishWorkerReservation(next, runtime))
}

func TestPartialEphemeralFillRetainsCleanupOwnership(t *testing.T) {
	for _, cleanupFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "removed", true: "removal fails"}[cleanupFails], func(t *testing.T) {
			runtime := &systemChecksTestRuntime{concurrentSelectionRuntime: concurrentSelectionFixture()}
			var removals int
			runtime.worker = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
				if strings.Contains(command, "fallocate") {
					return "", errors.New("response lost after partial allocation")
				}
				assert.Contains(t, command, "rm -f '/tmp/soperator-acceptance-ephemeral-fill'")
				removals++
				if cleanupFails {
					return "", errors.New("container not found")
				}
				return "", nil
			})
			kubectl, slurm := framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime)
			steps := NewSystemChecks(runtime, slurm, kubectl, framework.NewWorkerSelector(kubectl, slurm, "soperator"))
			ctx := framework.WithWorkerReservation(t.Context(), framework.NewWorkerPool(), t.Name())
			require.NoError(t, steps.aHealthyWorkerPodIsSelected(ctx))
			steps.workerPod.Spec.Containers = []corev1.Container{{Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse("10Gi")},
			}}}
			require.ErrorContains(t, steps.podLocalEphemeralStorageIsFilledAboveTheWarningThreshold(ctx), "partial allocation")
			steps.CleanupAndReset(ctx)
			assert.Equal(t, 1, removals)
			assert.False(t, steps.ephemeralFillOwned)
			err := framework.FinishWorkerReservation(ctx, runtime)
			if cleanupFails {
				require.ErrorContains(t, err, "container not found")
			} else {
				require.NoError(t, err)
			}
			steps.CleanupAndReset(ctx)
			assert.Equal(t, 1, removals, "reset cleanup must be idempotent")
		})
	}
}

func TestRestartedWorkerWaitsForFreshSlurmRegistration(t *testing.T) {
	for _, initial := range []string{
		"State=IDLE SlurmdStartTime=2026-10-29T12:00:00",
		"State=IDLE+NOT_RESPONDING SlurmdStartTime=2026-10-30T12:00:00",
	} {
		t.Run(initial, func(t *testing.T) {
			runtime := &systemChecksTestRuntime{concurrentSelectionRuntime: concurrentSelectionFixture()}
			var queries int
			runtime.controller = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
				require.Equal(t, "scontrol show node 'worker-0'", command)
				queries++
				if queries == 1 {
					return "NodeName=worker-0 " + initial, nil
				}
				return "NodeName=worker-0 State=IDLE SlurmdStartTime=2026-10-30T12:00:00", nil
			})
			steps := NewSystemChecks(runtime, framework.NewSlurmClient(runtime), nil, nil)
			steps.worker = framework.WorkerInfo{Name: "worker-0", SlurmNode: framework.SlurmNodeInfo{
				SlurmdStartTime: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC),
			}}
			steps.workerPod = runtime.pods[0]
			steps.restartedWorkerPodUID = "old-pod"
			steps.libslurmProbeResults = make(chan libslurmProbeResult)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			require.NoError(t, steps.theRestartedWorkerPodIsReady(ctx))
			assert.Equal(t, 2, queries, "a Ready replacement Pod must not release a stale or unresponsive Slurm worker")
		})
	}
}
