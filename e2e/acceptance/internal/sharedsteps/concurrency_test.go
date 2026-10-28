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
