package sharedsteps

import (
	"context"
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

const simulatedHealthDrainReason = "[node_problem] gpu_health_check: inject_error.inject_error: Injecting error [hc_program]"

type healthMaintenanceRuntime struct {
	*maintenanceTestRuntime
	jobState       string
	restarts       string
	patchErr       error
	workerCommands []string
}

func (r *healthMaintenanceRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		if args[0] == "patch" && r.patchErr != nil {
			r.calls = append(r.calls, "kubectl "+strings.Join(args, " "))
			return "", r.patchErr
		}
		return r.maintenanceTestRuntime.Kubectl().Run(ctx, args...)
	})
}

func (r *healthMaintenanceRuntime) Worker(framework.WorkerInfo) framework.CommandScope {
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r.workerCommands = append(r.workerCommands, command)
		return "", nil
	})
}

func healthMaintenanceSteps(t *testing.T) (*NodeReplacement, *healthMaintenanceRuntime) {
	t.Helper()
	rollout, base := maintenanceTestSteps(t)
	runtime := &healthMaintenanceRuntime{maintenanceTestRuntime: base, jobState: "RUNNING", restarts: "0"}
	steps := rollout.targets[0].replacement
	steps.runtime = runtime
	steps.slurm = framework.NewSlurmClient(runtime)
	steps.kubectl = framework.NewKubectlClient(runtime)
	steps.replacementWorker.HasGPU = true
	steps.maintenanceJob = framework.SbatchJob{ID: "6", JobName: "e2e-node-replacement"}
	base.command = func(command string) (string, error) {
		switch {
		case command == "scontrol show config":
			return "HealthCheckInterval = 120 sec", nil
		case strings.HasPrefix(command, "squeue "):
			return runtime.jobState, nil
		case strings.HasPrefix(command, "sacct "):
			state := runtime.jobState
			if state == "" {
				state = "CANCELLED"
			}
			return "6|" + state + "|0:0|None|", nil
		case strings.HasPrefix(command, "scontrol -o show job "):
			return "Restarts=" + runtime.restarts, nil
		case strings.HasPrefix(command, "scontrol show node "):
			name := strings.Trim(strings.TrimPrefix(command, "scontrol show node "), "'")
			return base.nodes[name], nil
		case strings.HasPrefix(command, "scancel "):
			runtime.jobState = ""
			return "", nil
		case strings.Contains(command, "sbatch "):
			return "6", nil
		case strings.HasPrefix(command, "tail -n "):
			return "test job output", nil
		default:
			return "", fmt.Errorf("unexpected command: %s", command)
		}
	}
	setHealthMaintenanceNode(runtime, "MIXED+DRAIN", 1, 128)
	return steps, runtime
}

func setHealthMaintenanceNode(runtime *healthMaintenanceRuntime, state string, cpu, memory uint64) {
	runtime.nodes["worker-0"] = fmt.Sprintf("NodeName=worker-0 State=%s CPUAlloc=%d AllocMem=%d InstanceId=worker-0-host Reason=%s",
		state, cpu, memory, simulatedHealthDrainReason)
}

func maintenancePatchCount(runtime *healthMaintenanceRuntime) int {
	var count int
	for _, call := range runtime.calls {
		if strings.HasPrefix(call, "kubectl patch node ") {
			count++
		}
	}
	return count
}

func TestNodeReplacementFullyDrained(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		want   bool
	}{
		{"idle drained", "State=IDLE+DRAIN CPUAlloc=0 AllocMem=0", true},
		{"allocated", "State=ALLOCATED+DRAIN CPUAlloc=1 AllocMem=128", false},
		{"mixed", "State=MIXED+DRAIN CPUAlloc=1 AllocMem=128", false},
		{"completing", "State=IDLE+DRAIN+COMPLETING CPUAlloc=0 AllocMem=0", false},
		{"remaining CPUs", "State=IDLE+DRAIN CPUAlloc=1 AllocMem=0", false},
		{"remaining memory", "State=IDLE+DRAIN CPUAlloc=0 AllocMem=128", false},
		{"unknown CPUs", "State=IDLE+DRAIN AllocMem=0", false},
		{"unknown memory", "State=IDLE+DRAIN CPUAlloc=0", false},
		{"undrained", "State=IDLE CPUAlloc=0 AllocMem=0", false},
		{"down", "State=DOWN+DRAIN CPUAlloc=0 AllocMem=0", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, nodeReplacementFullyDrained(framework.ParseSlurmNodeInfo("worker-0", test.output)))
		})
	}
}

func TestHealthMaintenanceUsesExistingTriggersWhileJobKeepsRunning(t *testing.T) {
	steps, runtime := healthMaintenanceSteps(t)
	require.NoError(t, steps.theNodeIsDrainingWithANodeProblemReasonWhileTheTestJobIsRunning(t.Context()))
	require.NoError(t, steps.aMaintenanceEventIsTriggeredWhileThatNodeIsDraining(t.Context()))
	require.NoError(t, steps.theNodeIsDrainingWithAMaintenanceReasonWhileTheTestJobIsRunning(t.Context()))

	assert.Equal(t, "RUNNING", runtime.jobState)
	assert.Equal(t, 1, maintenancePatchCount(runtime))
	assert.True(t, steps.maintenanceRequested)
	assert.Contains(t, runtime.nodes["worker-0"], "State=MIXED+DRAIN")
	assert.Contains(t, runtime.nodes["worker-0"], nodeReplacementDrainReason)
	assert.Equal(t, steps.original.PodUID, runtime.pods["worker-0"].UID)
}

func TestHealthMaintenanceRechecksJobImmediatelyBeforeInjection(t *testing.T) {
	steps, runtime := healthMaintenanceSteps(t)
	require.NoError(t, steps.theNodeIsDrainingWithANodeProblemReasonWhileTheTestJobIsRunning(t.Context()))
	runtime.jobState = ""

	err := steps.aMaintenanceEventIsTriggeredWhileThatNodeIsDraining(t.Context())

	require.ErrorContains(t, err, "test job 6 is not running without restarts")
	assert.Zero(t, maintenancePatchCount(runtime))
	assert.False(t, steps.maintenanceRequested)
}

func TestHealthMaintenanceRetriesTransientOriginalHostRead(t *testing.T) {
	steps, runtime := healthMaintenanceSteps(t)
	runtime.kube = func(...string) (string, error) {
		runtime.kube = nil
		return "", errors.New("temporary Kubernetes read error")
	}

	require.NoError(t, steps.theNodeIsDrainingWithANodeProblemReasonWhileTheTestJobIsRunning(t.Context()))
	assert.Equal(t, 2, runtime.iteration)
}

func TestHealthMaintenanceRejectsJobLossAndRestartsWithoutRetryingThem(t *testing.T) {
	for _, wait := range []string{"health", "maintenance"} {
		for _, test := range []struct {
			name, state, restarts string
		}{
			{"completed", "", "0"},
			{"completing", "COMPLETING", "0"},
			{"requeued", "PENDING", "1"},
			{"restarted", "RUNNING", "1"},
			{"unknown restart count", "RUNNING", ""},
		} {
			t.Run(wait+"/"+test.name, func(t *testing.T) {
				steps, runtime := healthMaintenanceSteps(t)
				runtime.nodes["worker-0"] = strings.Replace(runtime.nodes["worker-0"], simulatedHealthDrainReason, "waiting for check", 1)
				runtime.observe = func(base *maintenanceTestRuntime) {
					if base.iteration == 2 {
						runtime.jobState, runtime.restarts = test.state, test.restarts
					} else if base.iteration == 3 {
						runtime.jobState, runtime.restarts = "RUNNING", "0"
					}
				}
				var err error
				if wait == "health" {
					err = steps.theNodeIsDrainingWithANodeProblemReasonWhileTheTestJobIsRunning(t.Context())
				} else {
					err = steps.theNodeIsDrainingWithAMaintenanceReasonWhileTheTestJobIsRunning(t.Context())
				}
				var invariant rolloutInvariantError
				require.ErrorAs(t, err, &invariant)
				assert.ErrorContains(t, err, "test job 6 is not running without restarts")
				assert.Equal(t, 2, runtime.iteration)
				assert.Zero(t, maintenancePatchCount(runtime))
			})
		}
	}
}

func TestHealthMaintenanceWaitsForJobAndFullDrainBeforePatchingOriginalHost(t *testing.T) {
	steps, runtime := healthMaintenanceSteps(t)
	require.NoError(t, steps.theTestJobIsCancelled(t.Context()))
	assert.Equal(t, "6", steps.maintenanceJob.ID, "the fully drained step still needs the owned job ID")
	runtime.observe = func(base *maintenanceTestRuntime) {
		assert.Zero(t, maintenancePatchCount(runtime))
		if base.waitLabels[len(base.waitLabels)-1] != "original worker fully drained before maintenance" {
			return
		}
		switch base.iteration {
		case 1:
			setHealthMaintenanceNode(runtime, "MIXED+DRAIN", 1, 128)
		case 2:
			setHealthMaintenanceNode(runtime, "IDLE+DRAIN+COMPLETING", 0, 0)
		case 3:
			setHealthMaintenanceNode(runtime, "IDLE+DRAIN", 0, 0)
		}
	}

	require.NoError(t, steps.aMaintenanceEventIsTriggeredAfterThatNodeHasFullyDrained(t.Context()))

	assert.Equal(t, []string{"job 6 gone from queue", "original worker fully drained before maintenance"}, runtime.waitLabels)
	assert.Equal(t, 3, runtime.iteration)
	assert.Equal(t, 1, maintenancePatchCount(runtime))
	assert.True(t, steps.maintenanceRequested)
	assert.Contains(t, strings.Join(runtime.calls, "\n"), "kubectl patch node worker-0-host --subresource=status --type=strategic")
}

func TestHealthMaintenanceDoesNotInjectEventWhileCancelledJobRemainsQueued(t *testing.T) {
	steps, runtime := healthMaintenanceSteps(t)
	runtime.jobState = "COMPLETING"
	setHealthMaintenanceNode(runtime, "IDLE+DRAIN", 0, 0)

	err := steps.aMaintenanceEventIsTriggeredAfterThatNodeHasFullyDrained(t.Context())

	require.Error(t, err)
	assert.Equal(t, []string{"job 6 gone from queue"}, runtime.waitLabels)
	assert.Zero(t, maintenancePatchCount(runtime))
	assert.False(t, steps.maintenanceRequested)
}

func TestHealthMaintenanceRejectsChangedWorkerBeforeEitherEventTiming(t *testing.T) {
	for _, timing := range []string{"draining", "drained"} {
		for _, test := range []struct {
			name   string
			change func(*healthMaintenanceRuntime)
		}{
			{"host removed", func(r *healthMaintenanceRuntime) { delete(r.hosts, "worker-0-host") }},
			{"host UID reused", func(r *healthMaintenanceRuntime) {
				host := r.hosts["worker-0-host"]
				host.UID = "new-host-uid"
				r.hosts[host.Name] = host
			}},
			{"host deleting", func(r *healthMaintenanceRuntime) {
				host := r.hosts["worker-0-host"]
				host.DeletionTimestamp = new(metav1.Now())
				r.hosts[host.Name] = host
			}},
			{"pod replaced", func(r *healthMaintenanceRuntime) {
				pod := r.pods["worker-0"]
				pod.UID = "new-pod-uid"
				r.pods[pod.Name] = pod
			}},
			{"instance replaced", func(r *healthMaintenanceRuntime) {
				r.nodes["worker-0"] = strings.Replace(r.nodes["worker-0"], "InstanceId=worker-0-host", "InstanceId=new-instance", 1)
			}},
		} {
			t.Run(timing+"/"+test.name, func(t *testing.T) {
				steps, runtime := healthMaintenanceSteps(t)
				if timing == "drained" {
					runtime.jobState = ""
					setHealthMaintenanceNode(runtime, "IDLE+DRAIN", 0, 0)
				}
				test.change(runtime)
				var err error
				if timing == "draining" {
					err = steps.aMaintenanceEventIsTriggeredWhileThatNodeIsDraining(t.Context())
				} else {
					err = steps.aMaintenanceEventIsTriggeredAfterThatNodeHasFullyDrained(t.Context())
				}
				var invariant rolloutInvariantError
				require.ErrorAs(t, err, &invariant)
				assert.Zero(t, maintenancePatchCount(runtime))
				assert.False(t, steps.maintenanceRequested)
				if timing == "drained" {
					assert.Equal(t, 1, runtime.iteration, "replacement must fail immediately instead of waiting for a different host")
				}
			})
		}
	}
}

func TestHealthMaintenancePatchFailureRetainsOwnershipAndIsNotRetried(t *testing.T) {
	for _, timing := range []string{"draining", "drained"} {
		t.Run(timing, func(t *testing.T) {
			steps, runtime := healthMaintenanceSteps(t)
			runtime.patchErr = errors.New("patch response lost")
			var err error
			if timing == "draining" {
				err = steps.aMaintenanceEventIsTriggeredWhileThatNodeIsDraining(t.Context())
			} else {
				runtime.jobState = ""
				setHealthMaintenanceNode(runtime, "IDLE+DRAIN", 0, 0)
				err = steps.aMaintenanceEventIsTriggeredAfterThatNodeHasFullyDrained(t.Context())
			}
			require.ErrorIs(t, err, runtime.patchErr)
			assert.Equal(t, 1, maintenancePatchCount(runtime))
			assert.True(t, steps.maintenanceRequested)
		})
	}
}

func TestHealthMaintenanceCleanupHandlesBothOwnedTriggersAfterCancellation(t *testing.T) {
	steps, runtime := healthMaintenanceSteps(t)
	steps.maintenanceRequested, steps.faultInjected = true, true
	host := runtime.hosts[steps.original.NodeName]
	host.Status.Conditions = []corev1.NodeCondition{{Type: nodeReplacementMaintenanceCondition, Status: corev1.ConditionTrue, Reason: "AcceptanceTest"}}
	runtime.hosts[host.Name] = host
	runtime.local = func(...string) (string, error) { return "", errors.New("instance not found") }
	runtime.observe = func(base *maintenanceTestRuntime) {
		if base.waitLabels[len(base.waitLabels)-1] != "replacement worker ready on a new host" {
			return
		}
		delete(base.hosts, steps.original.NodeName)
		pod := base.pods[steps.original.PodName]
		pod.UID, pod.Spec.NodeName = "replacement-pod-uid", "replacement-host"
		base.pods[pod.Name] = pod
		base.nodes["worker-0"] = "NodeName=worker-0 State=IDLE InstanceId=replacement-instance"
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	steps.CleanupAndReset(ctx)
	steps.CleanupAndReset(ctx)

	assert.Contains(t, runtime.calls, "scancel '6'")
	assert.Equal(t, []string{"rm -f '/opt/soperator-outputs/local/health_checker_inject_error'"}, runtime.workerCommands)
	assert.False(t, steps.maintenanceRequested)
	assert.False(t, steps.faultInjected)
	assert.Zero(t, steps.original)
	assert.True(t, steps.maintenanceJob.IsZero())
	assert.Zero(t, maintenancePatchCount(runtime), "cleanup uses existing completion behavior without injecting another event")
}

func TestNodeReplacementHoldJobCoversBothDrainWaits(t *testing.T) {
	steps, runtime := healthMaintenanceSteps(t)
	setHealthMaintenanceNode(runtime, "IDLE", 0, 0)
	err := steps.submitTestJobOnWorker(t.Context(), steps.replacementWorker, framework.WorkerSnapshot{})
	require.NoError(t, err)
	assert.Greater(t, nodeReplacementJobHold, nodeReplacementHealthCheckDrainMaxWait+nodeReplacementDrainTimeout+nodeReplacementJobTimeout)
	assert.Contains(t, strings.Join(runtime.calls, "\n"), "--wrap='sleep 1800'")
	assert.Equal(t, "6", steps.maintenanceJob.ID)
	assert.LessOrEqual(t, nodeReplacementJobHold, 30*time.Minute)
}
