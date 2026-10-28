package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

type healthCheckReplacementRuntime struct {
	framework.Runtime
	kubectl    framework.ArgsScope
	controller framework.CommandScope
	worker     framework.CommandScope
	local      framework.ArgsScope
	wait       func(context.Context, func(context.Context) (bool, error)) error
	workerName string
}

func (r healthCheckReplacementRuntime) Kubectl() framework.ArgsScope       { return r.kubectl }
func (r healthCheckReplacementRuntime) Controller() framework.CommandScope { return r.controller }
func (r healthCheckReplacementRuntime) Worker(worker framework.WorkerInfo) framework.CommandScope {
	if r.workerName != "" && worker.Name != r.workerName {
		panic("command targets a worker outside the selected reservation")
	}
	return r.worker
}
func (r healthCheckReplacementRuntime) Local() framework.ArgsScope { return r.local }
func (r healthCheckReplacementRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	return r.wait(ctx, condition)
}
func (r healthCheckReplacementRuntime) Logf(string, ...any) {}

func TestHealthCheckInjectErrorCommandWritesResolutionToNodeLocalMarker(t *testing.T) {
	assert.Equal(t,
		`mkdir -p '/opt/soperator-outputs/local' && printf '%s\n' 'RedeployProfile' > '/opt/soperator-outputs/local/health_checker_inject_error'`,
		healthCheckInjectErrorCommand())
}

func TestSimulateHealthCheckFailureWritesMarkerOnSelectedWorker(t *testing.T) {
	var commands []string
	runtime := healthCheckReplacementRuntime{workerName: "worker-1", worker: framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		require.NoError(t, ctx.Err())
		commands = append(commands, command)
		return "", nil
	})}
	steps := NodeReplacement{runtime: runtime, replacementWorker: framework.WorkerInfo{Name: "worker-1"}}

	require.NoError(t, steps.aGPUHealthCheckFailureIsSimulatedOnThatNode(t.Context()))

	assert.Equal(t, []string{healthCheckInjectErrorCommand()}, commands)
	assert.True(t, steps.faultInjected)
}

func TestSimulateHealthCheckFailureKeepsOwnershipOnFailure(t *testing.T) {
	writeErr := errors.New("ssh: connection refused")
	runtime := healthCheckReplacementRuntime{worker: framework.NewCommandScope(func(context.Context, string) (string, error) {
		return "", writeErr
	})}
	steps := NodeReplacement{runtime: runtime, replacementWorker: framework.WorkerInfo{Name: "worker-1"}}

	err := steps.aGPUHealthCheckFailureIsSimulatedOnThatNode(t.Context())

	assert.ErrorIs(t, err, writeErr)
	assert.ErrorContains(t, err, "simulate GPU health check failure on worker-1")
	assert.True(t, steps.faultInjected, "cleanup must still inspect the node after a lost response")
}

func TestRemoveHealthCheckInjectErrorMarkerReportsUnconfirmedRemoval(t *testing.T) {
	var commands []string
	removeErr := errors.New("node is gone")
	runtime := healthCheckReplacementRuntime{worker: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		commands = append(commands, command)
		return "", removeErr
	})}
	steps := NodeReplacement{runtime: runtime, replacementWorker: framework.WorkerInfo{Name: "worker-1"}}

	err := steps.removeHealthCheckInjectErrorMarker(t.Context())
	require.ErrorIs(t, err, removeErr)

	assert.Equal(t, []string{"rm -f '/opt/soperator-outputs/local/health_checker_inject_error' && test ! -e '/opt/soperator-outputs/local/health_checker_inject_error'"}, commands)
}

func TestFinishHealthCheckReplacementSkipsWaitWhenInjectionTookNoEffect(t *testing.T) {
	original := framework.WorkerReplacementSnapshot{WorkerName: "worker-0", NodeName: "original-host", NodeUID: "host-uid", InstanceID: "original-instance"}
	hostJSON, err := json.Marshal(corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: original.NodeName, UID: original.NodeUID}})
	require.NoError(t, err)
	var kubectlCalls [][]string
	runtime := healthCheckReplacementRuntime{
		worker: framework.NewCommandScope(func(context.Context, string) (string, error) { return "", nil }),
		kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
			kubectlCalls = append(kubectlCalls, args)
			return string(hostJSON), nil
		}),
		controller: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
			require.Equal(t, "scontrol show node 'worker-0'", command)
			return "NodeName=worker-0 InstanceId=original-instance State=IDLE", nil
		}),
	}
	steps := NodeReplacement{
		runtime:           runtime,
		slurm:             framework.NewSlurmClient(runtime),
		kubectl:           framework.NewKubectlClient(runtime),
		original:          original,
		replacementWorker: framework.WorkerInfo{Name: original.WorkerName},
	}

	require.NoError(t, steps.finishHealthCheckReplacement(t.Context()))

	require.Len(t, kubectlCalls, 1, "an untouched host must not start the replacement wait")
	assert.Equal(t, []string{"get", "node", "original-host", "--ignore-not-found", "-o", "json"}, kubectlCalls[0])
}

func TestFinishHealthCheckReplacementVerifiesFaultRemovalOrReplacement(t *testing.T) {
	removeErr := errors.New("SSH response lost")
	waitErr := errors.New("replacement recovery timed out")
	queryErr := errors.New("forbidden")
	for _, tc := range []struct {
		name      string
		hostState string
		nodeState string
		removeErr error
		queryErr  error
		waitErr   error
		wantErr   error
		wantWait  bool
	}{
		{name: "failed removal on unchanged worker", removeErr: removeErr, wantErr: removeErr},
		{name: "cannot inspect original host", removeErr: removeErr, queryErr: queryErr, wantErr: queryErr},
		{name: "original host gone", hostState: "gone", removeErr: removeErr, wantWait: true},
		{name: "original host name reused", hostState: "reused", removeErr: removeErr, wantWait: true},
		{name: "original host being deleted", hostState: "deleting", wantWait: true},
		{name: "original worker drained", nodeState: "DRAIN", wantWait: true},
		{name: "Slurm already reports replacement", nodeState: "replaced", wantWait: true},
		{name: "replacement recovery fails", hostState: "gone", removeErr: removeErr, waitErr: waitErr, wantErr: waitErr, wantWait: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := framework.WorkerReplacementSnapshot{
				Namespace: "soperator", WorkerName: "worker-0", PodName: "worker-0", PodUID: "original-pod-uid",
				NodeName: "original-host", NodeUID: "host-uid", InstanceID: "original-instance",
			}
			var waited bool
			runtime := healthCheckReplacementRuntime{
				worker: framework.NewCommandScope(func(context.Context, string) (string, error) { return "", tc.removeErr }),
				kubectl: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
					var object any
					if args[1] == "node" {
						if tc.queryErr != nil {
							return "", tc.queryErr
						}
						if waited || tc.hostState == "gone" {
							return "", nil
						}
						host := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: original.NodeName, UID: original.NodeUID}}
						if tc.hostState == "reused" {
							host.UID = "different-uid"
						}
						if tc.hostState == "deleting" {
							host.DeletionTimestamp = new(metav1.Now())
						}
						object = host
					} else {
						require.Equal(t, "pod", args[1])
						pod := readyWorkerPod("worker-0", "new-pod-uid", nil)
						pod.Namespace, pod.Spec.Hostname, pod.Spec.NodeName = "soperator", "worker-0", "new-host"
						object = pod
					}
					output, err := json.Marshal(object)
					return string(output), err
				}),
				controller: framework.NewCommandScope(func(context.Context, string) (string, error) {
					instance, state := original.InstanceID, "IDLE"
					if tc.nodeState == "DRAIN" && !waited {
						state = "IDLE+DRAIN"
					}
					if waited || tc.nodeState == "replaced" {
						instance = "replacement-instance"
					}
					return fmt.Sprintf("NodeName=worker-0 InstanceId=%s State=%s", instance, state), nil
				}),
				local: framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
					require.Contains(t, args, original.InstanceID)
					return "", errors.New("instance not found")
				}),
				wait: func(ctx context.Context, condition func(context.Context) (bool, error)) error {
					waited = true
					if tc.waitErr != nil {
						return tc.waitErr
					}
					done, err := condition(ctx)
					require.NoError(t, err)
					require.True(t, done, "wait must confirm Pod, host, Slurm and Compute replacement")
					return nil
				},
			}
			steps := NodeReplacement{runtime: runtime, kubectl: framework.NewKubectlClient(runtime), slurm: framework.NewSlurmClient(runtime),
				original: original, replacementWorker: framework.WorkerInfo{Name: original.WorkerName}}
			err := steps.finishHealthCheckReplacement(t.Context())
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				if tc.removeErr != nil {
					assert.ErrorIs(t, err, tc.removeErr)
				}
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantWait, waited)
		})
	}
}

func TestHealthCheckCleanupFailureQuarantinesWorkerAfterCancellation(t *testing.T) {
	base := concurrentSelectionFixture()
	removeErr := errors.New("fault marker remains")
	runtime := healthCheckReplacementRuntime{
		Runtime: base, kubectl: base.Kubectl(),
		controller: framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
			if strings.HasPrefix(command, "scontrol show node ") {
				return "NodeName=worker-0 InstanceId=original-instance State=IDLE", nil
			}
			return base.Controller().Run(ctx, command)
		}),
		worker: framework.NewCommandScope(func(ctx context.Context, _ string) (string, error) {
			require.NoError(t, ctx.Err(), "cancellation must not prevent cleanup")
			return "", removeErr
		}),
	}
	selector := framework.NewWorkerSelector(framework.NewKubectlClient(runtime), framework.NewSlurmClient(runtime), "soperator")
	pool := framework.NewWorkerPool()
	ctx := framework.WithWorkerReservation(t.Context(), pool, t.Name())
	workers, err := selector.Acquire(ctx, framework.WorkerRequirements{Names: []string{"worker-0"}, Count: 1})
	require.NoError(t, err)
	steps := NodeReplacement{runtime: runtime, kubectl: framework.NewKubectlClient(runtime), slurm: framework.NewSlurmClient(runtime),
		replacementWorker: workers[0], faultInjected: true,
		original: framework.WorkerReplacementSnapshot{WorkerName: "worker-0", NodeName: "host-0", NodeUID: "host-0", InstanceID: "original-instance"}}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	steps.CleanupAndReset(cancelled)
	require.ErrorContains(t, framework.FinishWorkerReservation(ctx, runtime), removeErr.Error())
	assert.False(t, steps.faultInjected, "reset scenario state even when cleanup reports a failure")
	other := framework.WithWorkerReservation(t.Context(), pool, "next scenario")
	_, err = selector.Acquire(other, framework.WorkerRequirements{Names: []string{"worker-0"}, Count: 1})
	require.ErrorContains(t, err, "quarantined=")
}

func TestHealthCheckDrainTimeoutFollowsHealthCheckInterval(t *testing.T) {
	for _, tt := range []struct {
		name     string
		interval string
		want     time.Duration
		wantErr  string
	}{
		{name: "default interval", interval: "120 sec", want: 4 * time.Minute},
		{name: "short interval floors at the minimum", interval: "10 sec", want: 2 * time.Minute},
		{name: "at the limit", interval: "300 sec", want: 10 * time.Minute},
		{name: "too long", interval: "301 sec", wantErr: "too long for this scenario"},
		{name: "disabled", interval: "0 sec", wantErr: "HealthCheckProgram is disabled"},
		{name: "unexpected format", interval: "2 min", wantErr: "parse HealthCheckInterval"},
		{name: "not a number", interval: "abc sec", wantErr: "parse HealthCheckInterval"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			timeout, err := healthCheckDrainTimeout(map[string]string{"HealthCheckInterval": tt.interval})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, timeout)
		})
	}
}

func TestHealthCheckDrainTimeoutRequiresHealthCheckInterval(t *testing.T) {
	_, err := healthCheckDrainTimeout(map[string]string{"SlurmdTimeout": "180 sec"})
	require.ErrorContains(t, err, "HealthCheckInterval is missing")
}
