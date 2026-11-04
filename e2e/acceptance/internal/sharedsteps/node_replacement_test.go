package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
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
}

func (r healthCheckReplacementRuntime) Kubectl() framework.ArgsScope       { return r.kubectl }
func (r healthCheckReplacementRuntime) Controller() framework.CommandScope { return r.controller }
func (r healthCheckReplacementRuntime) Worker(framework.WorkerInfo) framework.CommandScope {
	return r.worker
}
func (r healthCheckReplacementRuntime) Logf(string, ...any) {}

func nodeEventsJSON(t *testing.T, events ...corev1.Event) string {
	t.Helper()
	raw, err := json.Marshal(corev1.EventList{Items: events})
	require.NoError(t, err)
	return string(raw)
}

func TestPlatformReportedDeletingTheOldNodeFindsRemediatorEvent(t *testing.T) {
	var args [][]string
	runtime := healthCheckReplacementRuntime{kubectl: framework.NewArgsScope(func(_ context.Context, a ...string) (string, error) {
		args = append(args, a)
		return nodeEventsJSON(t,
			corev1.Event{Reason: "NodeRemediationInProgress", Source: corev1.EventSource{Component: "nebiusmachinehealthcheck-controller"}},
			corev1.Event{Reason: "NodeDeleting", Message: "Deleting node", Source: corev1.EventSource{Component: "remediation-controller"}},
		), nil
	})}
	steps := NodeReplacement{
		runtime:  runtime,
		kubectl:  framework.NewKubectlClient(runtime),
		original: framework.WorkerReplacementSnapshot{NodeName: "computeinstance-old", NodeUID: "uid-old"},
	}

	require.NoError(t, steps.thePlatformReportedDeletingTheOldNode(t.Context()))

	require.Len(t, args, 1)
	assert.Equal(t, []string{"get", "events", "-n", "default",
		"--field-selector", "involvedObject.kind=Node,involvedObject.name=computeinstance-old,involvedObject.uid=uid-old",
		"-o", "json"}, args[0])
}

func TestPlatformReportedDeletingTheOldNodeListsObservedEventsOnFailure(t *testing.T) {
	runtime := healthCheckReplacementRuntime{kubectl: framework.NewArgsScope(func(context.Context, ...string) (string, error) {
		return nodeEventsJSON(t,
			corev1.Event{Type: "Normal", Reason: "NodeRemediationInProgress", Message: "Remediation is in progress",
				Source: corev1.EventSource{Component: "nebiusmachinehealthcheck-controller"}},
		), nil
	})}
	steps := NodeReplacement{
		runtime:  runtime,
		kubectl:  framework.NewKubectlClient(runtime),
		original: framework.WorkerReplacementSnapshot{NodeName: "computeinstance-old", NodeUID: "uid-old"},
	}

	err := steps.thePlatformReportedDeletingTheOldNode(t.Context())

	require.Error(t, err)
	assert.ErrorContains(t, err, "did not report deleting node computeinstance-old (UID uid-old)")
	assert.ErrorContains(t, err, "Normal/NodeRemediationInProgress from nebiusmachinehealthcheck-controller: Remediation is in progress")
}

func TestPlatformReportedDeletingTheOldNodeReportsNoEvents(t *testing.T) {
	runtime := healthCheckReplacementRuntime{kubectl: framework.NewArgsScope(func(context.Context, ...string) (string, error) {
		return nodeEventsJSON(t), nil
	})}
	steps := NodeReplacement{
		runtime:  runtime,
		kubectl:  framework.NewKubectlClient(runtime),
		original: framework.WorkerReplacementSnapshot{NodeName: "computeinstance-old", NodeUID: "uid-old"},
	}

	err := steps.thePlatformReportedDeletingTheOldNode(t.Context())

	assert.ErrorContains(t, err, "observed events: none")
}

func TestPlatformReportedDeletingTheOldNodeRequiresCapturedNode(t *testing.T) {
	steps := NodeReplacement{}
	assert.ErrorContains(t, steps.thePlatformReportedDeletingTheOldNode(t.Context()), "original node is not captured")
}

func TestHealthCheckInjectErrorCommandWritesResolutionToNodeLocalMarker(t *testing.T) {
	assert.Equal(t,
		`mkdir -p '/opt/soperator-outputs/local' && printf '%s\n' 'RedeployProfile' > '/opt/soperator-outputs/local/health_checker_inject_error'`,
		healthCheckInjectErrorCommand())
}

func TestSimulateHealthCheckFailureWritesMarkerOnSelectedWorker(t *testing.T) {
	var commands []string
	runtime := healthCheckReplacementRuntime{worker: framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
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

func TestRemoveHealthCheckInjectErrorMarkerIsBestEffort(t *testing.T) {
	var commands []string
	runtime := healthCheckReplacementRuntime{worker: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		commands = append(commands, command)
		return "", errors.New("node is gone")
	})}
	steps := NodeReplacement{runtime: runtime, replacementWorker: framework.WorkerInfo{Name: "worker-1"}}

	steps.removeHealthCheckInjectErrorMarker(t.Context())

	assert.Equal(t, []string{"rm -f '/opt/soperator-outputs/local/health_checker_inject_error'"}, commands)
}

func TestFinishHealthCheckReplacementSkipsWaitWhenInjectionTookNoEffect(t *testing.T) {
	original := framework.WorkerReplacementSnapshot{WorkerName: "worker-0", NodeName: "original-host", NodeUID: "host-uid", InstanceID: "original-instance"}
	hostJSON, err := json.Marshal(corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: original.NodeName, UID: original.NodeUID}})
	require.NoError(t, err)
	var kubectlCalls [][]string
	runtime := healthCheckReplacementRuntime{
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
		runtime:  runtime,
		slurm:    framework.NewSlurmClient(runtime),
		kubectl:  framework.NewKubectlClient(runtime),
		original: original,
	}

	require.NoError(t, steps.finishHealthCheckReplacement(t.Context()))

	require.Len(t, kubectlCalls, 1, "an untouched host must not start the replacement wait")
	assert.Equal(t, []string{"get", "node", "original-host", "--ignore-not-found", "-o", "json"}, kubectlCalls[0])
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
