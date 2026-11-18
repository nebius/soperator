package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type stuckJobTestRuntime struct {
	rolloutPollRuntime
	kubectl    framework.ArgsScope
	controller framework.CommandScope
	jail       framework.CommandScope
	logs       []string
}

func (r *stuckJobTestRuntime) Kubectl() framework.ArgsScope       { return r.kubectl }
func (r *stuckJobTestRuntime) Controller() framework.CommandScope { return r.controller }
func (r *stuckJobTestRuntime) Jail() framework.CommandScope       { return r.jail }
func (r *stuckJobTestRuntime) Logf(format string, args ...any) {
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}

func stuckJobTestSteps(t *testing.T, state, boot string) (*StuckJobRecovery, *stuckJobTestRuntime) {
	t.Helper()
	runtime := &stuckJobTestRuntime{}
	host := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "host", UID: "host-uid"},
		Status: corev1.NodeStatus{
			NodeInfo:   corev1.NodeSystemInfo{BootID: "before"},
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	}
	runtime.controller = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		require.Equal(t, "scontrol show node 'worker-0'", command)
		return "NodeName=worker-0 InstanceId=instance-0 State=" + state, nil
	})
	runtime.kubectl = framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		if args[1] == "node" {
			observed := host.DeepCopy()
			observed.Status.NodeInfo.BootID = boot
			data, err := json.Marshal(observed)
			return string(data), err
		}
		require.Equal(t, "pods", args[1])
		return `{"items":[{"metadata":{"name":"worker-0"},"spec":{"hostname":"worker-0","nodeName":"host"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`, nil
	})
	steps := NewStuckJobRecovery(runtime, framework.NewSlurmClient(runtime), framework.NewKubectlClient(runtime), nil)
	steps.worker = framework.WorkerInfo{Name: "worker-0", HasGPU: true, SlurmNode: framework.SlurmNodeInfo{InstanceID: "instance-0", GPUCount: 4}}
	steps.host = host
	return steps, runtime
}

func TestStuckJobReservationCreatesItsOwnScopeAndSkipsUnconfiguredGPUs(t *testing.T) {
	runtime := &stuckJobTestRuntime{}
	runtime.controller = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		switch {
		case strings.HasPrefix(command, "sinfo "):
			return "worker-cpu-0", nil
		case command == "scontrol -o show reservations":
			return "", nil
		default:
			return "NodeName=worker-cpu-0 State=IDLE InstanceId=cpu-instance", nil
		}
	})
	runtime.kubectl = framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		if args[1] == "nodesets" {
			return `{"items":[{"metadata":{"name":"worker-cpu"},"spec":{"clusterName":"soperator","replicas":1}}]}`, nil
		}
		return `{"items":[]}`, nil
	})
	kubectl, slurm := framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime)
	steps := NewStuckJobRecovery(runtime, slurm, kubectl, framework.NewWorkerSelector(kubectl, slurm, "soperator"))
	ctx, err := steps.reserveWorker(t.Context())
	require.ErrorIs(t, err, godog.ErrSkip)
	require.NoError(t, framework.FinishWorkerReservation(ctx, runtime))
}

func TestStuckJobRebootRequiresDownAndChangedBootID(t *testing.T) {
	for _, tt := range []struct {
		name, state, boot string
		wantError         bool
	}{
		{name: "reboot", state: "DOWN+CLOUD+DRAIN", boot: "after"},
		{name: "no DOWN", state: "IDLE+CLOUD", boot: "after", wantError: true},
		{name: "no reboot", state: "DOWN+CLOUD+DRAIN", boot: "before", wantError: true},
		{name: "missing boot ID", state: "DOWN", wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			steps, _ := stuckJobTestSteps(t, tt.state, tt.boot)
			err := steps.waitForReboot(t.Context())
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestStuckJobRecoveryRequiresHealthyIdleWorkerAfterReboot(t *testing.T) {
	for _, tt := range []struct {
		name, state, boot string
		wantError         bool
	}{
		{name: "recovered", state: "IDLE+CLOUD+RESERVED", boot: "after"},
		{name: "still drained", state: "IDLE+DRAIN", boot: "after", wantError: true},
		{name: "still down", state: "DOWN", boot: "after", wantError: true},
		{name: "not rebooted", state: "IDLE", boot: "before", wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			steps, _ := stuckJobTestSteps(t, tt.state, tt.boot)
			err := steps.waitForRecovery(t.Context())
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestStuckJobRebootRejectsReplacement(t *testing.T) {
	for _, change := range []string{"instance", "host"} {
		t.Run(change, func(t *testing.T) {
			steps, _ := stuckJobTestSteps(t, "IDLE", "after")
			node := steps.worker.SlurmNode
			host := *steps.host.DeepCopy()
			if change == "instance" {
				node.InstanceID = "replacement-instance"
			} else {
				host.UID = "replacement-host"
			}
			err := steps.checkHostIdentity(node, host, true)
			var permanent *framework.PermanentError
			require.ErrorAs(t, err, &permanent)
			assert.ErrorContains(t, err, "replaced instead of rebooted")
		})
	}
}

func TestStuckJobLostDrainResponseStillRequiresCleanup(t *testing.T) {
	steps, runtime := stuckJobTestSteps(t, "IDLE", "after")
	commandErr := errors.New("lost response")
	runtime.controller = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		assert.Contains(t, command, "'nodename=worker-0' 'state=DRAIN' 'reason=Kill task failed'")
		return "", commandErr
	})
	require.ErrorIs(t, steps.simulateDrain(t.Context()), commandErr)
	assert.True(t, steps.faultInjected)
}

func TestStuckJobCleanupWaitsForRecoveryAndResetsState(t *testing.T) {
	for _, state := range []string{"IDLE", "IDLE+DRAIN"} {
		t.Run(state, func(t *testing.T) {
			steps, runtime := stuckJobTestSteps(t, state, "after")
			steps.faultInjected = true
			steps.CleanupAndReset(t.Context())
			assert.Empty(t, steps.worker.Name)
			assert.False(t, steps.faultInjected)
			assert.Equal(t, state != "IDLE", strings.Contains(strings.Join(runtime.logs, "\n"), "Cleanup: recover"))
			runtime.logs = nil
			steps.CleanupAndReset(t.Context())
			assert.Empty(t, runtime.logs)
		})
	}
}

func TestStuckJobGPUJobRequestsAndChecksEveryGPU(t *testing.T) {
	for _, visible := range []int{3, 4} {
		t.Run(fmt.Sprint(visible), func(t *testing.T) {
			steps, runtime := stuckJobTestSteps(t, "IDLE", "after")
			runtime.jail = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
				assert.Contains(t, command, "'--gpus-per-node=4'")
				assert.Contains(t, command, "'--nodelist=worker-0'")
				return strings.Repeat("GPU 0: NVIDIA H200\n", visible), nil
			})
			err := steps.runGPUJob(t.Context())
			if visible == 4 {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "sees 3 GPUs, expected 4")
			}
		})
	}
}
