package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type concurrentSelectionRuntime struct {
	framework.Runtime
	pods  []corev1.Pod
	debug bool
}

type partitionSmokeRuntime struct {
	*concurrentSelectionRuntime
	jail framework.CommandScope
}

func (r *partitionSmokeRuntime) Jail() framework.CommandScope { return r.jail }

func TestPartitionSmokeJobsReserveTheirWorkerUntilScenarioCleanup(t *testing.T) {
	for _, partition := range []string{"default", "hidden"} {
		t.Run(partition, func(t *testing.T) {
			runtime := &partitionSmokeRuntime{concurrentSelectionRuntime: concurrentSelectionFixture()}
			kubectl, slurm := framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime)
			selector := framework.NewWorkerSelector(kubectl, slurm, "soperator")
			pool := framework.NewWorkerPool()
			restarting := framework.WithWorkerReservation(t.Context(), pool, "restarting worker")
			_, err := selector.Acquire(restarting, framework.WorkerRequirements{Names: []string{"worker-0"}, Count: 1})
			require.NoError(t, err)
			defer func() { require.NoError(t, framework.FinishWorkerReservation(restarting, runtime)) }()

			var command string
			runtime.jail = framework.NewCommandScope(func(_ context.Context, value string) (string, error) {
				command = value
				return "worker-1\n", nil
			})
			ctx := framework.WithWorkerReservation(t.Context(), pool, "partition smoke")
			steps := NewClusterCreation(&framework.ClusterInfo{}, runtime, kubectl, selector)
			if partition == "default" {
				require.NoError(t, steps.checkMainSmokeJob(ctx))
				assert.NotContains(t, command, " -p ", "the default partition must still be selected by Slurm")
			} else {
				require.NoError(t, steps.checkHiddenSmokeJob(ctx))
				assert.Contains(t, command, " -p 'hidden' ")
			}
			assert.Contains(t, command, "--nodelist='worker-1'", "the smoke job must avoid the other scenario's worker")

			steps.CleanupAndReset(ctx)
			other := framework.WithWorkerReservation(t.Context(), pool, "next scenario")
			bounded, cancel := context.WithTimeout(other, 20*time.Millisecond)
			defer cancel()
			_, err = selector.Acquire(bounded, framework.WorkerRequirements{Names: []string{"worker-1"}, Count: 1})
			require.ErrorIs(t, err, context.DeadlineExceeded, "the reservation must survive step-family cleanup")

			require.NoError(t, framework.FinishWorkerReservation(ctx, runtime))
			workers, err := selector.Acquire(other, framework.WorkerRequirements{Names: []string{"worker-1"}, Count: 1})
			require.NoError(t, err)
			assert.Equal(t, "worker-1", workers[0].Name)
			require.NoError(t, framework.FinishWorkerReservation(other, runtime))
		})
	}
}

func TestReadinessSmokeJobsReuseTheirScenarioWorker(t *testing.T) {
	runtime := &partitionSmokeRuntime{concurrentSelectionRuntime: concurrentSelectionFixture()}
	var commands []string
	runtime.jail = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		commands = append(commands, command)
		return "worker-0\n", nil
	})
	kubectl, slurm := framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime)
	selector := framework.NewWorkerSelector(kubectl, slurm, "soperator")
	ctx := framework.WithWorkerReservation(t.Context(), framework.NewWorkerPool(), t.Name())
	steps := NewClusterCreation(&framework.ClusterInfo{}, runtime, kubectl, selector)
	require.NoError(t, steps.checkMainSmokeJob(ctx))
	require.NoError(t, steps.checkHiddenSmokeJob(ctx))
	require.Len(t, commands, 2)
	for _, command := range commands {
		assert.Contains(t, command, "--nodelist='worker-0'")
	}
	require.NoError(t, framework.FinishWorkerReservation(ctx, runtime))
	steps.CleanupAndReset(ctx)
}

func (r *concurrentSelectionRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		var object any
		switch strings.Join(args[:2], " ") {
		case "get nodesets":
			return `{"items":[{"metadata":{"name":"worker"},"spec":{"replicas":2,"gpu":{"enabled":true}}}]}`, nil
		case "get pods":
			object = corev1.PodList{Items: r.pods}
		case "get pod":
			for _, pod := range r.pods {
				if pod.Name == args[4] {
					object = pod
				}
			}
		case "get nodes":
			var nodes corev1.NodeList
			for _, pod := range r.pods {
				nodes.Items = append(nodes.Items, concurrentSelectionHost(pod.Spec.NodeName))
			}
			object = nodes
		case "get node":
			object = concurrentSelectionHost(args[2])
		case "debug node/host-0":
			r.debug = true
			return "", nil
		default:
			return "", fmt.Errorf("unexpected kubectl arguments: %v", args)
		}
		output, err := json.Marshal(object)
		return string(output), err
	})
}

func concurrentSelectionHost(name string) corev1.Node {
	return corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
}

func (*concurrentSelectionRuntime) Controller() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		if strings.HasPrefix(command, "sinfo ") {
			return "worker-0\nworker-1", nil
		}
		return "NodeName=worker-0 State=IDLE CfgTRES=gres/gpu=8\nNodeName=worker-1 State=IDLE CfgTRES=gres/gpu=8", nil
	})
}

func (*concurrentSelectionRuntime) Logf(string, ...any) {}

func (*concurrentSelectionRuntime) WaitFor(ctx context.Context, description string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	done, err := condition(ctx)
	if err != nil {
		return err
	}
	if !done {
		return fmt.Errorf("%s remains pending", description)
	}
	return nil
}

func concurrentSelectionFixture() *concurrentSelectionRuntime {
	runtime := &concurrentSelectionRuntime{}
	for index := range 2 {
		name := fmt.Sprintf("worker-%d", index)
		pod := readyWorkerPod(name, types.UID(name), nil)
		pod.Spec.Hostname = name
		pod.Spec.NodeName = fmt.Sprintf("host-%d", index)
		runtime.pods = append(runtime.pods, pod)
	}
	return runtime
}

func TestNewWorkerSelectorsRespectOtherScenarioReservations(t *testing.T) {
	for _, kind := range []string{"Docker", "Docker GPU", "NCCL debug"} {
		t.Run(kind, func(t *testing.T) {
			runtime := concurrentSelectionFixture()
			kubectl, slurm := framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime)
			selector := framework.NewWorkerSelector(kubectl, slurm, "soperator")
			pool := framework.NewWorkerPool()
			other := framework.WithWorkerReservation(t.Context(), pool, "other scenario")
			held, err := selector.PickGPUWorkers(other, 1)
			require.NoError(t, err)
			defer func() { require.NoError(t, framework.FinishWorkerReservation(other, runtime)) }()
			ctx := framework.WithWorkerReservation(t.Context(), pool, t.Name())
			defer func() { require.NoError(t, framework.FinishWorkerReservation(ctx, runtime)) }()
			var selected framework.WorkerInfo
			if kind == "NCCL debug" {
				steps := NewNCCLDebug(runtime, slurm, kubectl, selector)
				require.NoError(t, steps.selectWorker(ctx))
				selected = steps.worker
			} else {
				steps := NewSystemSettings(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, slurm, kubectl, selector)
				if kind == "Docker" {
					require.NoError(t, steps.selectHealthyDockerWorker(ctx))
				} else {
					require.NoError(t, steps.selectHealthyDockerGPUWorker(ctx))
				}
				selected = steps.worker
			}
			assert.NotEqual(t, held[0].Name, selected.Name, "selection must not use another scenario's idle but reserved worker")
		})
	}
}

func TestKubeletFaultRejectsHostWithUnreservedWorker(t *testing.T) {
	runtime := concurrentSelectionFixture()
	runtime.pods[1].Spec.NodeName = "host-0"
	kubectl, slurm := framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime)
	selector := framework.NewWorkerSelector(kubectl, slurm, "soperator")
	ctx := framework.WithWorkerReservation(t.Context(), framework.NewWorkerPool(), t.Name())
	workers, err := selector.Acquire(ctx, framework.WorkerRequirements{Names: []string{"worker-0"}, Count: 1})
	require.NoError(t, err)
	defer func() { require.NoError(t, framework.FinishWorkerReservation(ctx, runtime)) }()
	steps := NewSystemChecks(runtime, slurm, kubectl, selector)
	steps.worker = workers[0]
	steps.workerPodInfo = framework.WorkerPodInfo{PodName: "worker-0"}
	err = steps.kubeletIsStoppedOnTheSelectedWorkerKubernetesNode(ctx)
	require.ErrorContains(t, err, "worker worker-1 is outside this scenario's reservation")
	assert.False(t, runtime.debug, "host ownership must be checked before stopping kubelet")
}
