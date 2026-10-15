package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

type rolloutPollRuntime struct{ framework.Runtime }

type maintenancePatchRuntime struct {
	framework.Runtime
	kubectl framework.ArgsScope
}

func (r maintenancePatchRuntime) Kubectl() framework.ArgsScope { return r.kubectl }

func TestTriggerNodeMaintenanceUsesScopedStrategicStatusPatch(t *testing.T) {
	for _, originalStep := range []bool{false, true} {
		t.Run(fmt.Sprintf("original step=%t", originalStep), func(t *testing.T) {
			var calls [][]string
			runtime := maintenancePatchRuntime{kubectl: framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
				require.NoError(t, ctx.Err())
				calls = append(calls, args)
				return "", nil
			})}
			var err error
			if originalStep {
				steps := NodeReplacement{runtime: runtime, original: framework.WorkerReplacementSnapshot{
					NodeName: "selected-host", InstanceID: "different-instance-id",
				}}
				err = steps.aMaintenanceEventIsTriggeredForThatNode(t.Context())
			} else {
				err = triggerNodeMaintenance(t.Context(), runtime, "selected-host")
			}
			require.NoError(t, err)
			require.Len(t, calls, 1)
			require.Len(t, calls[0], 7)
			assert.Equal(t, []string{"patch", "node", "selected-host", "--subresource=status", "--type=strategic", "-p"}, calls[0][:6])
			var patch map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(calls[0][6]), &patch))
			require.Len(t, patch, 1, "the patch must modify only status")
			var status corev1.NodeStatus
			require.NoError(t, json.Unmarshal(patch["status"], &status))
			require.Len(t, status.Conditions, 1, "strategic merging must preserve unrelated condition types")
			condition := status.Conditions[0]
			assert.Equal(t, corev1.NodeConditionType(nodeReplacementMaintenanceCondition), condition.Type)
			assert.Equal(t, corev1.ConditionTrue, condition.Status)
			assert.Equal(t, "AcceptanceTest", condition.Reason)
			assert.Equal(t, "Maintenance scheduled for node", condition.Message)
			assert.WithinDuration(t, time.Now().UTC(), condition.LastTransitionTime.Time, time.Second)
		})
	}
}

func TestTriggerNodeMaintenancePropagatesPatchFailure(t *testing.T) {
	patchErr := errors.New("target context denied patch")
	runtime := maintenancePatchRuntime{kubectl: framework.NewArgsScope(func(context.Context, ...string) (string, error) {
		return "", patchErr
	})}
	err := triggerNodeMaintenance(t.Context(), runtime, "selected-host")
	assert.ErrorIs(t, err, patchErr)
	assert.ErrorContains(t, err, "patch maintenance condition")
}

type rolloutJobRuntime struct {
	rolloutPollRuntime
	submission      string
	submissions     []string
	submissionError error
}

type rolloutCleanupRuntime struct {
	framework.Runtime
	commands framework.CommandScope
	kube     framework.ArgsScope
	waits    []string
	logs     []string
}

type rolloutCleanupArgsScope struct{ framework.ArgsScope }

func (s rolloutCleanupArgsScope) RunWithDefaultRetry(ctx context.Context, args ...string) (string, error) {
	return s.Run(ctx, args...)
}

func (r *rolloutCleanupRuntime) Jail() framework.CommandScope       { return r.commands }
func (r *rolloutCleanupRuntime) Controller() framework.CommandScope { return r.commands }
func (r *rolloutCleanupRuntime) Kubectl() framework.ArgsScope       { return r.kube }
func (r *rolloutCleanupRuntime) Logf(format string, args ...any) {
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}
func (r *rolloutCleanupRuntime) WaitFor(ctx context.Context, description string, timeout, interval time.Duration, condition func(context.Context) (bool, error)) error {
	r.waits = append(r.waits, description)
	return (&rolloutPollRuntime{}).WaitFor(ctx, description, timeout, interval, condition)
}

func TestWorkerRolloutCleanupCancelsJobsAfterScenarioCancellation(t *testing.T) {
	for _, id := range []string{"123", ""} {
		t.Run("job ID "+id, func(t *testing.T) {
			var calls []string
			runtime := &rolloutCleanupRuntime{commands: framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
				require.NoError(t, ctx.Err(), "cleanup must still stop jobs and remove gates after scenario cancellation")
				deadline, bounded := ctx.Deadline()
				require.True(t, bounded)
				assert.WithinDuration(t, time.Now().Add(workerRolloutCleanupTimeout), deadline, time.Second)
				calls = append(calls, command)
				return "", nil
			})}
			steps := WorkerRollingUpdate{runtime: runtime, slurm: framework.NewSlurmClient(runtime),
				targets: []*workerRolloutTarget{{replacement: NewNodeReplacement(runtime, nil, nil),
					job: workerRolloutJob{job: framework.SbatchJob{ID: id, JobName: "owned-rollout"}, releasePath: "/scenario/release"}}}}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			steps.CleanupAndReset(ctx)
			cancellation := "scancel --name='owned-rollout'"
			if id != "" {
				cancellation = "scancel '123'"
			}
			query := "squeue -h --name='owned-rollout' -o '%i'"
			if id != "" {
				query = "squeue -h -j '123' -o '%T'"
			}
			assert.Equal(t, []string{cancellation, "rm -f -- '/scenario/release'", query}, calls)
			assert.Empty(t, steps.targets)
		})
	}
}

func TestWorkerRolloutCleanupCancelsBothWorkerJobs(t *testing.T) {
	var calls []string
	runtime := &rolloutCleanupRuntime{commands: framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		require.NoError(t, ctx.Err())
		calls = append(calls, command)
		return "", nil
	})}
	steps := WorkerRollingUpdate{runtime: runtime, slurm: framework.NewSlurmClient(runtime), targets: []*workerRolloutTarget{
		{replacement: NewNodeReplacement(runtime, nil, nil), job: workerRolloutJob{job: framework.SbatchJob{ID: "123"}, releasePath: "/scenario/first.release"}},
		{replacement: NewNodeReplacement(runtime, nil, nil), job: workerRolloutJob{job: framework.SbatchJob{JobName: "second-owned-job"}, releasePath: "/scenario/second.release"}},
	}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	steps.CleanupAndReset(ctx)
	assert.Equal(t, []string{"scancel '123'", "rm -f -- '/scenario/first.release'",
		"scancel --name='second-owned-job'", "rm -f -- '/scenario/second.release'",
		"squeue -h -j '123' -o '%T'", "squeue -h --name='second-owned-job' -o '%i'"}, calls)
	assert.Empty(t, steps.targets)
}

func TestWorkerRolloutCleanupRestoresChecksAfterScenarioCancellation(t *testing.T) {
	runtime, deployment, release, calls := checksIntervalFixture(t)
	originalArgs := append([]string(nil), deployment.Spec.Template.Spec.Containers[0].Args...)
	kubectl := framework.NewKubectlClient(runtime)
	state, err := prepareMaintenanceChecksInterval(t.Context(), runtime, kubectl)
	require.NoError(t, err)
	scope := runtime.kube
	runtime.kube = rolloutCleanupArgsScope{framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		require.NoError(t, ctx.Err(), "restoration must survive cancellation of the scenario")
		_, bounded := ctx.Deadline()
		require.True(t, bounded)
		return scope.Run(ctx, args...)
	})}
	steps := WorkerRollingUpdate{runtime: runtime, kubectl: kubectl, checksInterval: state}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	steps.CleanupAndReset(ctx)
	assert.Equal(t, originalArgs, deployment.Spec.Template.Spec.Containers[0].Args)
	assert.Nil(t, release.Spec.Suspend)
	assert.Equal(t, "original", release.Metadata.Annotations[helmDriftAnnotation])
	assert.Nil(t, steps.checksInterval)
	assert.Empty(t, runtime.logs)
	count := len(*calls)
	steps.CleanupAndReset(ctx)
	assert.Len(t, *calls, count, "cleanup after reset must leave the cluster untouched")
}

func TestWorkerRolloutCleanupWaitsForLostSubmissionAndLogsJobsThatRemain(t *testing.T) {
	for _, stuck := range []bool{false, true} {
		t.Run(fmt.Sprintf("stuck=%t", stuck), func(t *testing.T) {
			queries := 0
			runtime := &rolloutCleanupRuntime{}
			runtime.commands = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
				if strings.HasPrefix(command, "squeue ") {
					queries++
					if stuck || queries == 1 {
						return "123", nil
					}
				}
				return "", nil
			})
			steps := WorkerRollingUpdate{runtime: runtime, slurm: framework.NewSlurmClient(runtime), targets: []*workerRolloutTarget{
				{job: workerRolloutJob{job: framework.SbatchJob{JobName: "unique-scenario-worker-0"}}},
			}}
			steps.CleanupAndReset(t.Context())
			if stuck {
				require.Len(t, runtime.logs, 1)
				assert.Contains(t, runtime.logs[0], "name=unique-scenario-worker-0")
				assert.Contains(t, runtime.logs[0], "still queued: 123")
			} else {
				assert.Equal(t, 2, queries, "an acknowledged scancel does not prove the lost job has stopped")
				assert.Empty(t, runtime.logs)
			}
			assert.Empty(t, steps.targets)
		})
	}
}

func TestWorkerRolloutCleanupSettlesCurrentTemplateBeforeReleasingHolds(t *testing.T) {
	for _, annotation := range []string{"requested", "rejected", "external"} {
		t.Run(annotation, func(t *testing.T) {
			runtime := &rolloutCleanupRuntime{}
			current := kubeobjects.NodeSet{
				Metadata: kubeobjects.ObjectMeta{Name: "worker", Namespace: framework.SoperatorNamespace},
				Spec:     kubeobjects.NodeSetSpec{Replicas: 1, WorkerAnnotations: map[string]string{workerRolloutAnnotationKey: annotation}},
				Status:   kubeobjects.NodeSetStatus{Phase: kubeobjects.NodeSetPhaseReady, Replicas: 1},
			}
			pod := readyWorkerPod("worker-0", "old-pod", map[string]string{workerRolloutAnnotationKey: "rejected"})
			pod.Spec.NodeName = "host-0"
			pending := annotation != "rejected"
			reads := 0
			runtime.kube = framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
				require.NoError(t, ctx.Err())
				var object any
				switch args[1] {
				case "nodeset":
					object = current
				case "pods":
					reads++
					object = corev1.PodList{Items: []corev1.Pod{*pod.DeepCopy()}}
					pod.UID = "new-pod"
					pod.Annotations[workerRolloutAnnotationKey] = annotation
					pending = false
				case "nodes":
					object = corev1.NodeList{Items: []corev1.Node{readyWorkerMK8sHost("host-0", "group-1")}}
				default:
					return "", fmt.Errorf("unexpected kubectl command %v", args)
				}
				data, err := json.Marshal(object)
				return string(data), err
			})
			held := true
			runtime.commands = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
				switch command {
				case "scontrol -o show nodes":
					return "NodeName=worker-0 State=IDLE CPUAlloc=0", nil
				case "scontrol show config":
					if held {
						return "SuspendExcNodes = worker-0", nil
					}
					return "SuspendExcNodes = (null)", nil
				case "scontrol show hostnames 'worker-0'":
					return "worker-0", nil
				case "scontrol update 'SuspendExcNodes-=worker-0'":
					require.False(t, pending, "keep workers protected until their installed template is healthy")
					held = false
					return "", nil
				default:
					return "", fmt.Errorf("unexpected Slurm command %s", command)
				}
			})
			steps := WorkerRollingUpdate{
				runtime: runtime, kubectl: framework.NewKubectlClient(runtime), slurm: framework.NewSlurmClient(runtime),
				nodeSet: current, annotationValue: "requested", initialPods: map[string]workerPodSnapshot{"worker-0": {UID: "old-pod"}},
				power: workerRolloutPower{names: []string{"worker-0"}, added: []string{"worker-0"}},
			}
			steps.nodeSet.Spec.WorkerAnnotations = map[string]string{workerRolloutAnnotationKey: "rejected"}
			steps.CleanupAndReset(t.Context())
			require.False(t, held)
			if annotation == "rejected" {
				assert.Equal(t, 1, reads, "a rejected patch already has a healthy baseline")
			} else {
				assert.Equal(t, 2, reads, "cleanup must settle the installed template rather than replay failed assertions")
			}
			if annotation == "external" {
				require.Len(t, runtime.logs, 1)
				assert.Contains(t, runtime.logs[0], "changed externally")
			} else {
				assert.Empty(t, runtime.logs)
			}
			waits := len(runtime.waits)
			steps.CleanupAndReset(t.Context())
			assert.Len(t, runtime.waits, waits, "reset cleanup must perform no additional polling")
			assert.Empty(t, steps.annotationValue)
			assert.Equal(t, kubeobjects.NodeSet{}, steps.nodeSet)
		})
	}
}

func TestWorkerRolloutCleanupSettlesPartiallyRestartedDaemonSets(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(fmt.Sprintf("accepted restart response lost=%t", accepted), func(t *testing.T) {
			runtime := &rolloutCleanupRuntime{}
			daemonSet := appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "system-worker", Generation: 3},
				Status: appsv1.DaemonSetStatus{ObservedGeneration: 3, DesiredNumberScheduled: 1, UpdatedNumberScheduled: 1, NumberAvailable: 1}}
			reads := 0
			runtime.kube = rolloutCleanupArgsScope{framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
				if args[0] == "rollout" {
					if accepted {
						daemonSet.Generation = 4
						daemonSet.Status.UpdatedNumberScheduled = 0
					}
					return "", fmt.Errorf("restart response lost")
				}
				reads++
				data, err := json.Marshal(appsv1.DaemonSetList{Items: []appsv1.DaemonSet{daemonSet}})
				if reads > 1 {
					daemonSet.Status.ObservedGeneration = daemonSet.Generation
					daemonSet.Status.UpdatedNumberScheduled = 1
				}
				return string(data), err
			})}
			steps := WorkerRollingUpdate{runtime: runtime, kubectl: framework.NewKubectlClient(runtime)}
			require.ErrorContains(t, steps.restartSystemDaemonSets(t.Context()), "restart response lost")
			assert.Equal(t, []string{"system-worker"}, steps.restartedDaemonSets, "record restart ownership before its response")
			steps.CleanupAndReset(t.Context())
			if accepted {
				assert.Equal(t, 3, reads, "wait for the accepted restart's current generation to converge")
			} else {
				assert.Equal(t, 2, reads, "do not require a generation increase after a rejected restart")
			}
			assert.Empty(t, runtime.logs)
			assert.Empty(t, steps.restartedDaemonSets)
			steps.CleanupAndReset(t.Context())
			assert.Len(t, runtime.waits, 1)
		})
	}
}

type rolloutWorkersRuntime struct {
	framework.Runtime
	nodeSet       kubeobjects.NodeSet
	nodeSets      []kubeobjects.NodeSet
	active        []int32
	pods          []corev1.Pod
	podsByNodeSet map[string][]corev1.Pod
	nodes         []corev1.Node
	kubeResponses map[string]string
	instanceIDs   map[string]string
	submissions   []string
	local         func(...string) (string, error)
	afterKubeRead func(...string)
	calls         []string
}

type rolloutPowerReply struct {
	command string
	output  string
	err     error
}

type rolloutPowerRuntime struct {
	framework.Runtime
	t       *testing.T
	replies []rolloutPowerReply
	logs    []string
}

type rolloutPowerScope struct {
	framework.CommandScope
	r *rolloutPowerRuntime
}

func readyWorkerPod(name string, uid types.UID, annotations map[string]string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: uid, Annotations: annotations},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
}

func TestWorkerRolloutCandidateProblem(t *testing.T) {
	ready := kubeobjects.NodeSet{
		Spec: kubeobjects.NodeSetSpec{
			ClusterName:    "soperator",
			Replicas:       2,
			GPU:            kubeobjects.NodeSetGPUSpec{Enabled: true},
			UpdateStrategy: kubeobjects.NodeSetUpdateStrategySlurmAwareRollingUpdate,
		},
		Status: kubeobjects.NodeSetStatus{Phase: kubeobjects.NodeSetPhaseReady, Replicas: 2},
	}
	assert.Empty(t, workerRolloutCandidateProblem(ready, "soperator", 2))

	tests := []struct {
		name   string
		mutate func(*kubeobjects.NodeSet)
		want   string
	}{
		{"other cluster", func(n *kubeobjects.NodeSet) { n.Spec.ClusterName = "other" }, "belongs to another cluster"},
		{"cpu", func(n *kubeobjects.NodeSet) { n.Spec.GPU.Enabled = false }, ""},
		{"strategy omitted", func(n *kubeobjects.NodeSet) { n.Spec.UpdateStrategy = "" }, `updateStrategy=""`},
		{"kruise rolling update", func(n *kubeobjects.NodeSet) { n.Spec.UpdateStrategy = "rollingUpdate" }, `updateStrategy="rollingUpdate"`},
		{"ephemeral", func(n *kubeobjects.NodeSet) { n.Spec.EphemeralNodes = new(true) }, ""},
		{"no replicas", func(n *kubeobjects.NodeSet) { n.Spec.Replicas = 0 }, "no replicas"},
		{"not ready", func(n *kubeobjects.NodeSet) { n.Status.Replicas = 1 }, "phase=Ready ready=1/2"},
		{"provisioning", func(n *kubeobjects.NodeSet) { n.Status.Phase = "Provisioning" }, "phase=Provisioning ready=2/2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeSet := ready
			tt.mutate(&nodeSet)
			assert.Equal(t, tt.want, workerRolloutCandidateProblem(nodeSet, "soperator", 2))
		})
	}
}

func TestDaemonSetRolledOut(t *testing.T) {
	rolled := appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Generation: 3},
		Status: appsv1.DaemonSetStatus{
			ObservedGeneration:     3,
			DesiredNumberScheduled: 4,
			UpdatedNumberScheduled: 4,
			NumberAvailable:        4,
		},
	}
	assert.True(t, daemonSetRolledOut(rolled, 2))

	tests := []struct {
		name   string
		mutate func(*appsv1.DaemonSet)
	}{
		{"generation not bumped", func(d *appsv1.DaemonSet) { d.Generation = 2 }},
		{"status lags", func(d *appsv1.DaemonSet) { d.Status.ObservedGeneration = 2 }},
		{"not all updated", func(d *appsv1.DaemonSet) { d.Status.UpdatedNumberScheduled = 3 }},
		{"not all available", func(d *appsv1.DaemonSet) { d.Status.NumberAvailable = 3 }},
		{"unavailable pods", func(d *appsv1.DaemonSet) { d.Status.NumberUnavailable = 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemonSet := rolled
			tt.mutate(&daemonSet)
			assert.False(t, daemonSetRolledOut(daemonSet, 2))
		})
	}
}

func TestWorkerRolloutRestartWaitsForDaemonSetGenerationAndReadiness(t *testing.T) {
	runtime := &rolloutCleanupRuntime{}
	daemonSet := appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "system-worker", Generation: 3},
		Status: appsv1.DaemonSetStatus{ObservedGeneration: 3, DesiredNumberScheduled: 1, UpdatedNumberScheduled: 1, NumberAvailable: 1}}
	reads, restarts := 0, 0
	runtime.kube = framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		if args[0] == "rollout" {
			assert.Equal(t, []string{"rollout", "restart", "daemonset", "system-worker", "-n", framework.SoperatorSystemNamespace}, args)
			restarts++
			daemonSet.Generation++
			daemonSet.Status.UpdatedNumberScheduled = 0
			return "", nil
		}
		assert.Equal(t, []string{"get", "daemonsets", "-n", framework.SoperatorSystemNamespace, "-o", "json"}, args,
			"the restart step should inspect DaemonSet rollout status")
		reads++
		if reads == 3 {
			daemonSet.Status.ObservedGeneration = daemonSet.Generation
		}
		if reads == 4 {
			daemonSet.Status.UpdatedNumberScheduled = 1
		}
		data, err := json.Marshal(appsv1.DaemonSetList{Items: []appsv1.DaemonSet{daemonSet}})
		return string(data), err
	})
	steps := WorkerRollingUpdate{runtime: runtime, kubectl: framework.NewKubectlClient(runtime),
		initialPods: map[string]workerPodSnapshot{"worker-0": {UID: "original", NodeName: "original-host"}}}
	require.NoError(t, steps.restartSystemDaemonSets(t.Context()))
	assert.Equal(t, 1, restarts)
	assert.Equal(t, 4, reads, "wait for the new generation to be observed and every scheduled Pod updated")
}

func TestRolloutJobsRequireSuccessfulTerminalRecordsWithoutKubernetesQueries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		jobID string
		state string
		exit  string
		want  string
	}{
		{name: "completed with unknown restart metadata", jobID: "7", state: "COMPLETED", exit: "0:0"},
		{name: "first job failed", jobID: "6", state: "NODE_FAIL", exit: "0:0", want: "state=NODE_FAIL"},
		{name: "second job cancelled", jobID: "7", state: "CANCELLED by 0", exit: "0:15", want: "state=CANCELLED by 0"},
		{name: "completed with nonzero exit", jobID: "7", state: "COMPLETED", exit: "1:0", want: "exit_code=1:0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var released []string
			var accounted []string
			runtime := &rolloutCleanupRuntime{}
			runtime.kube = framework.NewArgsScope(func(context.Context, ...string) (string, error) {
				t.Fatal("job completion must not depend on current worker Pods or Kubernetes handoff state")
				return "", nil
			})
			runtime.commands = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
				if strings.HasPrefix(command, "touch -- ") {
					released = append(released, command)
					return "", nil
				}
				require.Len(t, released, 2, "release both job gates before waiting for completion")
				switch {
				case strings.HasPrefix(command, "squeue "):
					return "", nil
				case strings.HasPrefix(command, "sacct "):
					id := "6"
					if strings.Contains(command, "'7'") {
						id = "7"
					}
					accounted = append(accounted, id)
					if id == tc.jobID {
						return id + "|" + tc.state + "|" + tc.exit + "|None|", nil
					}
					return id + "|COMPLETED|0:0|None|", nil
				case strings.HasPrefix(command, "scontrol -o show job "):
					return "", nil
				default:
					return "", fmt.Errorf("unexpected completion command: %s", command)
				}
			})
			steps := WorkerRollingUpdate{runtime: runtime, slurm: framework.NewSlurmClient(runtime),
				initialPods: map[string]workerPodSnapshot{"worker-0": {UID: "original-0"}, "worker-1": {UID: "original-1"}},
				targets: []*workerRolloutTarget{
					{worker: framework.WorkerInfo{Name: "worker-0"}, job: workerRolloutJob{job: framework.SbatchJob{ID: "6"}, releasePath: "/scenario/first.release"}},
					{worker: framework.WorkerInfo{Name: "worker-1"}, job: workerRolloutJob{job: framework.SbatchJob{ID: "7"}, releasePath: "/scenario/second.release"}},
				}}
			err := steps.finishRolloutJobs(t.Context())
			if tc.want == "" {
				require.NoError(t, err)
				assert.Contains(t, accounted, "6")
				assert.Contains(t, accounted, "7")
			} else {
				require.ErrorContains(t, err, tc.want)
			}
			assert.Equal(t, []string{"touch -- '/scenario/first.release'", "touch -- '/scenario/second.release'"}, released)
		})
	}
}

func TestRolloutIdleStepObservesOnlySelectedSlurmNodes(t *testing.T) {
	runtime := &rolloutCleanupRuntime{}
	runtime.kube = framework.NewArgsScope(func(context.Context, ...string) (string, error) {
		t.Fatal("the IDLE assertion must not inspect Kubernetes placement or handoff labels")
		return "", nil
	})
	reads := 0
	runtime.commands = framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		assert.Equal(t, "scontrol -o show nodes", command)
		reads++
		state := "ALLOCATED"
		if reads > 1 {
			state = "IDLE+CLOUD"
		}
		return "NodeName=worker-3 State=" + state + " Reason=stale\nNodeName=worker-8 State=IDLE\nNodeName=other-0 State=DOWN", nil
	})
	steps := WorkerRollingUpdate{runtime: runtime, slurm: framework.NewSlurmClient(runtime),
		workers:     []framework.WorkerInfo{{Name: "worker-3"}, {Name: "worker-8"}},
		nodeSet:     kubeobjects.NodeSet{Metadata: kubeobjects.ObjectMeta{Name: "worker"}},
		initialPods: map[string]workerPodSnapshot{"worker-3": {UID: "old-3"}, "worker-8": {UID: "old-8"}},
	}
	require.NoError(t, steps.waitForWorkersIdle(t.Context()))
	assert.Equal(t, 2, reads)
}

func TestIdleProblems(t *testing.T) {
	assert.Empty(t, idleProblems([]framework.SlurmNodeInfo{
		{Name: "worker-0", State: "IDLE"},
		{Name: "worker-1", State: "IDLE+CLOUD", Reason: "stale"},
	}))

	problems := idleProblems([]framework.SlurmNodeInfo{
		{Name: "worker-0", State: "IDLE+DRAIN", Reason: "soperator rolling update"},
		{Name: "worker-1", State: "MIXED"},
		{Name: "worker-2", State: "IDLE", Reason: "stale"},
		{Name: "worker-3", State: "IDLE"},
	})
	require.Len(t, problems, 2)
	assert.Contains(t, problems[0], "worker-0 state=IDLE+DRAIN")
	assert.Contains(t, problems[1], "worker-1 state=MIXED")
}

func TestWorkerRolloutBudget(t *testing.T) {
	for _, tc := range []struct {
		name           string
		value          *intstr.IntOrString
		replicas, want int
		wantErr        bool
	}{
		{"default", nil, 4, 1, false},
		{"integer", new(intstr.FromInt(2)), 4, 2, false},
		{"percentage floors", new(intstr.FromString("50%")), 3, 1, false},
		{"percentage allows two", new(intstr.FromString("75%")), 3, 2, false},
		{"percentage rounds to zero", new(intstr.FromString("10%")), 3, 1, false},
		{"clamped to replicas", new(intstr.FromInt(6)), 3, 3, false},
		{"zero budget follows controller minimum", new(intstr.FromInt(0)), 3, 1, false},
		{"invalid", new(intstr.FromString("bad")), 3, 0, true},
		{"empty NodeSet", nil, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := workerRolloutBudget(tc.value, tc.replicas)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestWorkerRolloutUnhealthyConfigurationIsNotOptional(t *testing.T) {
	nodeSet := kubeobjects.NodeSet{
		Spec:   kubeobjects.NodeSetSpec{Replicas: 2, GPU: kubeobjects.NodeSetGPUSpec{Enabled: true}, UpdateStrategy: kubeobjects.NodeSetUpdateStrategySlurmAwareRollingUpdate},
		Status: kubeobjects.NodeSetStatus{Phase: "Provisioning", Replicas: 1},
	}
	assert.Empty(t, workerRolloutConfigurationProblem(nodeSet, "soperator"))
	assert.NotEmpty(t, workerRolloutCandidateProblem(nodeSet, "soperator", 2))
}

func (r *rolloutJobRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		if strings.Contains(command, " && sbatch ") {
			r.submission = command
			r.submissions = append(r.submissions, command)
			if r.submissionError != nil {
				return "", r.submissionError
			}
			return strconv.Itoa(122 + len(r.submissions)), nil
		}
		if strings.HasPrefix(command, "squeue ") {
			return "RUNNING", nil
		}
		return "", fmt.Errorf("unexpected Slurm command: %s", command)
	})
}

func (*rolloutJobRuntime) Logf(string, ...any) {}

func TestRolloutJobGateCoversPreparationAndDrainPaths(t *testing.T) {
	preparation := 2 * workerRolloutJobStartWait
	maintenance := 2*nodeReplacementDrainTimeout + workerRolloutDrainTimeout
	assert.Greater(t, workerRolloutJobGateWait(), preparation+maintenance)
	assert.Greater(t, workerRolloutJobGateWait(), preparation+workerMK8sTimeout)
}

func TestRolloutJobSubmissionLeavesTimeToCompleteAfterGate(t *testing.T) {
	for _, workerCount := range []int{1, 11, 32} {
		t.Run(fmt.Sprintf("%d workers", workerCount), func(t *testing.T) {
			runtime := &rolloutJobRuntime{}
			workers := make([]framework.WorkerInfo, workerCount)
			workers[0].Name = "worker-0"
			steps := WorkerRollingUpdate{
				runtime: runtime, slurm: framework.NewSlurmClient(runtime), workers: workers,
				nodeSet: kubeobjects.NodeSet{Spec: kubeobjects.NodeSetSpec{GPU: kubeobjects.NodeSetGPUSpec{Enabled: true}}},
			}

			gateWait := workerRolloutJobGateWait()
			owned, err := steps.submitRolloutJob(t.Context(), workers[0], gateWait)
			require.NoError(t, err)
			var timeLimitMinutes int
			for _, field := range strings.Fields(runtime.submission) {
				if value, ok := strings.CutPrefix(field, "--time="); ok {
					var err error
					timeLimitMinutes, err = strconv.Atoi(value)
					require.NoError(t, err)
				}
			}
			assert.Greater(t, time.Duration(timeLimitMinutes)*time.Minute, gateWait)
			assert.Contains(t, runtime.submission, "--wrap="+framework.ShellQuote(framework.JobReleaseGate(owned.releasePath, gateWait)))
			assert.Contains(t, runtime.submission, "--no-requeue")
			assert.Contains(t, runtime.submission, "--gpus-per-node=1")
		})
	}
}

func TestWorkerRolloutJobsHaveIndependentTargetsAndReleaseGates(t *testing.T) {
	runtime := &rolloutJobRuntime{}
	steps := WorkerRollingUpdate{runtime: runtime, slurm: framework.NewSlurmClient(runtime),
		nodeSet: kubeobjects.NodeSet{Spec: kubeobjects.NodeSetSpec{GPU: kubeobjects.NodeSetGPUSpec{Enabled: true}}}}
	gateWait := workerRolloutJobGateWait()
	first, err := steps.submitRolloutJob(t.Context(), framework.WorkerInfo{Name: "worker-3"}, gateWait)
	require.NoError(t, err)
	second, err := steps.submitRolloutJob(t.Context(), framework.WorkerInfo{Name: "worker-8"}, gateWait)
	require.NoError(t, err)
	require.Len(t, runtime.submissions, 2)
	assert.NotEqual(t, first.job.ID, second.job.ID)
	assert.NotEqual(t, first.job.JobName, second.job.JobName)
	assert.NotEqual(t, first.releasePath, second.releasePath)
	for i, target := range []struct {
		worker string
		owned  workerRolloutJob
	}{{"worker-3", first}, {"worker-8", second}} {
		assert.Contains(t, runtime.submissions[i], "--nodelist="+framework.ShellQuote(target.worker))
		assert.Contains(t, runtime.submissions[i], "--wrap="+framework.ShellQuote(framework.JobReleaseGate(target.owned.releasePath, gateWait)))
		assert.Contains(t, runtime.submissions[i], fmt.Sprintf("--time=%.0f", (gateWait+workerRolloutJobFinishWait).Minutes()))
	}
}

func TestWorkerRolloutJobRetainsCleanupOwnershipAfterLostSubmissionResponse(t *testing.T) {
	runtime := &rolloutJobRuntime{submissionError: errors.New("lost sbatch response")}
	steps := WorkerRollingUpdate{runtime: runtime, slurm: framework.NewSlurmClient(runtime)}
	owned, err := steps.submitRolloutJob(t.Context(), framework.WorkerInfo{Name: "worker-0"}, workerRolloutJobGateWait())
	require.ErrorContains(t, err, "lost sbatch response")
	assert.Empty(t, owned.job.ID)
	assert.NotEmpty(t, owned.job.JobName)
	assert.NotEmpty(t, owned.releasePath)
}

func TestRolloutWaitDoesNotRetrySafetyViolation(t *testing.T) {
	runtime := &rolloutPollRuntime{}
	steps := &WorkerRollingUpdate{runtime: runtime}
	calls := 0
	err := steps.waitForRollout(t.Context(), "test", time.Second, time.Millisecond, func(context.Context) (bool, error) {
		calls++
		if calls == 1 {
			return false, rolloutInvariant("worker moved while job was running")
		}
		return true, nil
	})
	require.ErrorContains(t, err, "worker moved")
	assert.Equal(t, 1, calls)
}

func TestRolloutWaitRetriesTransientObservation(t *testing.T) {
	runtime := &rolloutPollRuntime{}
	steps := &WorkerRollingUpdate{runtime: runtime}
	calls := 0
	err := steps.waitForRollout(t.Context(), "test", time.Second, time.Millisecond, func(context.Context) (bool, error) {
		calls++
		if calls == 1 {
			return false, fmt.Errorf("temporary API error")
		}
		return true, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func (*rolloutPollRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	var last error
	for range 3 {
		done, err := condition(ctx)
		last = err
		if done && err == nil {
			return nil
		}
	}
	return fmt.Errorf("poll exhausted: %w", last)
}

func rolloutWorkerTestNodeSet(ephemeral bool) kubeobjects.NodeSet {
	return kubeobjects.NodeSet{
		Metadata: kubeobjects.ObjectMeta{Name: "worker", Namespace: "soperator"},
		Spec: kubeobjects.NodeSetSpec{
			ClusterName: "soperator", Replicas: 10, EphemeralNodes: new(ephemeral),
			GPU: kubeobjects.NodeSetGPUSpec{Enabled: true}, UpdateStrategy: kubeobjects.NodeSetUpdateStrategySlurmAwareRollingUpdate,
			MaxUnavailable: new(intstr.FromInt(2)),
		},
		Status: kubeobjects.NodeSetStatus{Phase: kubeobjects.NodeSetPhaseReady, Replicas: 2},
	}
}

func TestRolloutWorkerNamesUseStaticReplicasOrSparseActiveOrdinals(t *testing.T) {
	static := rolloutWorkerTestNodeSet(false)
	static.Spec.Replicas = 3
	for _, enabled := range []*bool{nil, new(false)} {
		static.Spec.EphemeralNodes = enabled
		names, err := rolloutWorkerNames(static, []int32{2})
		require.NoError(t, err)
		assert.Equal(t, []string{"worker-0", "worker-1", "worker-2"}, names)
	}
	ephemeral := rolloutWorkerTestNodeSet(true)
	names, err := rolloutWorkerNames(ephemeral, []int32{8, 3})
	require.NoError(t, err)
	assert.Equal(t, []string{"worker-3", "worker-8"}, names)
	names, err = rolloutWorkerNames(ephemeral, nil)
	require.NoError(t, err)
	assert.Empty(t, names, "no active workers must not be replaced by configured capacity")
	for _, active := range [][]int32{{-1}, {10}, {3, 3}} {
		_, err := rolloutWorkerNames(ephemeral, active)
		require.ErrorContains(t, err, "duplicate or out-of-range ordinal")
	}
}

func TestRolloutRequiresExactActivePodMembership(t *testing.T) {
	names := []string{"worker-3", "worker-8"}
	pod := func(name string) corev1.Pod { return corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name}} }
	require.NoError(t, requireRolloutPodNames(names, []corev1.Pod{pod("worker-8"), pod("worker-3")}))
	for _, pods := range [][]corev1.Pod{
		{pod("worker-3")},
		{pod("worker-3"), pod("worker-4")},
		{pod("worker-3"), pod("worker-3")},
		{pod("worker-3"), pod("worker-8"), pod("worker-9")},
	} {
		require.ErrorContains(t, requireRolloutPodNames(names, pods), "match active worker pods")
	}
}

func TestSelectRolloutWorkersIgnoresInactiveSlurmNodes(t *testing.T) {
	pool := []framework.WorkerInfo{
		{Name: "worker-0", SlurmNode: framework.SlurmNodeInfo{State: "IDLE+CLOUD+POWERED_DOWN"}},
		{Name: "worker-3", SlurmNode: framework.SlurmNodeInfo{State: "IDLE+CLOUD"}},
		{Name: "worker-8", SlurmNode: framework.SlurmNodeInfo{State: "IDLE+CLOUD"}},
	}
	workers, err := selectRolloutWorkers([]string{"worker-3", "worker-8"}, pool)
	require.NoError(t, err)
	assert.Equal(t, pool[1:], workers)
	_, err = selectRolloutWorkers([]string{"worker-3", "worker-9"}, pool)
	require.ErrorContains(t, err, "find active worker worker-9")
	_, err = selectRolloutWorkers([]string{"worker-3"}, append(pool, pool[1]))
	require.ErrorContains(t, err, "find unique Slurm worker worker-3")
}

func TestSparseRolloutReadinessUsesActiveWorkers(t *testing.T) {
	nodeSet := rolloutWorkerTestNodeSet(true)
	assert.Empty(t, workerRolloutCandidateProblem(nodeSet, "soperator", 2))
	nodeSet.Status.Replicas = 1
	assert.Equal(t, "phase=Ready ready=1/2", workerRolloutCandidateProblem(nodeSet, "soperator", 2))
}

func TestRolloutIdleUsesSharedSchedulabilitySemantics(t *testing.T) {
	for _, state := range []string{"IDLE", "IDLE+CLOUD", "IDLE+CLOUD+POWERED_DOWN", "IDLE+POWERING_UP", "IDLE+REBOOT_REQUESTED"} {
		assert.Empty(t, idleProblems([]framework.SlurmNodeInfo{{Name: "worker-3", State: state, Reason: "stale but schedulable"}}))
	}
	for _, flag := range []string{"DRAIN", "DOWN", "FAIL", "NOT_RESPONDING", "INVALID_REG"} {
		assert.NotEmpty(t, idleProblems([]framework.SlurmNodeInfo{{Name: "worker-3", State: "IDLE+CLOUD+" + flag}}), flag)
	}
}

func (r *rolloutWorkersRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		r.calls = append(r.calls, "kubectl "+strings.Join(args, " "))
		if output, ok := r.kubeResponses[strings.Join(args, " ")]; ok {
			return output, nil
		}
		var object any
		switch args[1] {
		case "nodesets":
			nodeSets := r.nodeSets
			if len(nodeSets) == 0 {
				nodeSets = []kubeobjects.NodeSet{r.nodeSet}
			}
			object = kubeobjects.NodeSetList{Items: nodeSets}
		case "nodesetpowerstate":
			object = kubeobjects.NodeSetPowerState{Spec: kubeobjects.NodeSetPowerStateSpec{ActiveNodes: r.active}}
		case "pods":
			pods := r.pods
			if r.podsByNodeSet != nil {
				pods = nil
				for _, arg := range args {
					if name, ok := strings.CutPrefix(arg, "slurm.nebius.ai/nodeset="); ok {
						pods = r.podsByNodeSet[name]
						break
					}
				}
				if pods == nil {
					for _, nodeSet := range r.nodeSets {
						pods = append(pods, r.podsByNodeSet[nodeSet.Metadata.Name]...)
					}
				}
			}
			for _, arg := range args {
				if host, ok := strings.CutPrefix(arg, "spec.nodeName="); ok {
					var matching []corev1.Pod
					for _, pod := range pods {
						if pod.Spec.NodeName == host {
							matching = append(matching, pod)
						}
					}
					pods = matching
				}
			}
			object = corev1.PodList{Items: pods}
		case "pod":
			for _, pod := range r.pods {
				if pod.Name == args[2] {
					object = pod
				}
			}
			for _, pods := range r.podsByNodeSet {
				for _, pod := range pods {
					if pod.Name == args[2] {
						object = pod
					}
				}
			}
			if object == nil {
				return "", fmt.Errorf("unexpected pod: %v", args)
			}
		case "nodes":
			object = corev1.NodeList{Items: r.nodes}
		case "node":
			for _, node := range r.nodes {
				if node.Name == args[2] {
					object = node
				}
			}
			if object == nil {
				return "", fmt.Errorf("unexpected node: %v", args)
			}
		default:
			return "", fmt.Errorf("unexpected kubectl command: %v", args)
		}
		data, err := json.Marshal(object)
		if r.afterKubeRead != nil {
			r.afterKubeRead(args...)
		}
		return string(data), err
	})
}

func (r *rolloutWorkersRuntime) Controller() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		r.calls = append(r.calls, command)
		switch {
		case command == "scontrol show config":
			return "SuspendExcNodes = worker-0,worker-3\n", nil
		case strings.HasPrefix(command, "scontrol show hostnames "):
			return "worker-0\nworker-3\n", nil
		case command == "scontrol -o show nodes":
			nodeSets := r.nodeSets
			if len(nodeSets) == 0 {
				nodeSets = []kubeobjects.NodeSet{r.nodeSet}
			}
			var records []string
			for _, nodeSet := range nodeSets {
				for ordinal := range nodeSet.Spec.Replicas {
					name := fmt.Sprintf("%s-%d", nodeSet.Metadata.Name, ordinal)
					state := "IDLE"
					if len(r.nodeSets) == 0 {
						state = "IDLE+CLOUD+POWERED_DOWN"
						if ordinal == 0 || ordinal == 3 {
							state = "IDLE+CLOUD"
						}
					}
					records = append(records, "NodeName="+name+" State="+state+" InstanceId="+r.instanceID(name))
				}
			}
			return strings.Join(records, "\n"), nil
		case strings.HasPrefix(command, "sinfo "):
			var names []string
			if len(r.nodeSets) > 0 {
				for _, nodeSet := range r.nodeSets {
					for ordinal := range nodeSet.Spec.Replicas {
						names = append(names, fmt.Sprintf("%s-%d", nodeSet.Metadata.Name, ordinal))
					}
				}
			} else {
				for ordinal := range r.nodeSet.Spec.Replicas {
					names = append(names, fmt.Sprintf("worker-%d", ordinal))
				}
			}
			return strings.Join(names, "\n"), nil
		case strings.HasPrefix(command, "scontrol show node "):
			if len(r.nodeSets) > 0 {
				name := strings.Trim(strings.TrimPrefix(command, "scontrol show node "), "'")
				return "State=IDLE InstanceId=" + r.instanceID(name), nil
			}
			state := "IDLE+CLOUD+POWERED_DOWN"
			if command == "scontrol show node 'worker-0'" || command == "scontrol show node 'worker-3'" {
				state = "IDLE+CLOUD"
			}
			return "State=" + state, nil
		default:
			return "", fmt.Errorf("unexpected Slurm command: %s", command)
		}
	})
}

func (r *rolloutWorkersRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		r.calls = append(r.calls, command)
		switch {
		case strings.Contains(command, " && sbatch "):
			r.submissions = append(r.submissions, command)
			return strconv.Itoa(100 + len(r.submissions)), nil
		case strings.HasPrefix(command, "squeue "):
			return "RUNNING", nil
		case strings.HasPrefix(command, "timeout 180 srun "):
			return "", nil
		default:
			return "", fmt.Errorf("unexpected jail command: %s", command)
		}
	})
}

func (r *rolloutWorkersRuntime) Local() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		r.calls = append(r.calls, strings.Join(args, " "))
		if r.local == nil {
			return "", fmt.Errorf("unexpected local command: %v", args)
		}
		return r.local(args...)
	})
}

func (r *rolloutWorkersRuntime) instanceID(name string) string {
	if id, ok := r.instanceIDs[name]; ok {
		return id
	}
	return name + "-instance"
}

func (r *rolloutWorkersRuntime) WaitFor(ctx context.Context, description string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	done, err := condition(ctx)
	if err != nil {
		return err
	}
	if !done {
		return fmt.Errorf("%s remains pending", description)
	}
	return nil
}

func (*rolloutWorkersRuntime) Logf(string, ...any) {}

func TestSelectNodeSetSupportsSparseEphemeralWorkers(t *testing.T) {
	runtime := &rolloutWorkersRuntime{nodeSet: rolloutWorkerTestNodeSet(true), active: []int32{0, 3}}
	for _, ordinal := range runtime.active {
		name := fmt.Sprintf("worker-%d", ordinal)
		pod := readyWorkerPod(name, types.UID(name+"-uid"), nil)
		pod.ResourceVersion = "1"
		pod.Spec.NodeName = fmt.Sprintf("host-%d", ordinal)
		pod.Status.StartTime = new(metav1.Now())
		runtime.pods = append(runtime.pods, pod)
	}
	kubectl := framework.NewKubectlClient(runtime)
	slurm := framework.NewSlurmClient(runtime)
	steps := NewWorkerRollingUpdate(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, slurm, kubectl,
		framework.NewWorkerSelector(kubectl, slurm, "soperator"))
	require.NoError(t, steps.selectNodeSet(t.Context()))
	assert.Equal(t, []string{"worker-0", "worker-3"}, steps.workerNames)
	require.Len(t, steps.workers, 2)
	assert.Equal(t, "worker-3", steps.workers[1].Name)
	assert.Len(t, steps.initialPods, 2)
	assert.Empty(t, steps.power.added, "preexisting exclusions remain unowned")
	assert.NotContains(t, strings.Join(runtime.calls, "\n"), "scontrol update")

	runtime.active = []int32{0, 4}
	called := false
	err := steps.waitForRollout(t.Context(), "detect external power change", time.Minute, time.Second, func(context.Context) (bool, error) {
		called = true
		return true, nil
	})
	var invariant rolloutInvariantError
	require.ErrorAs(t, err, &invariant)
	assert.Contains(t, err.Error(), "active workers changed during rollout")
	assert.False(t, called, "active membership drift must fail before a successful condition can mask it")
}

func TestSelectNodeSetWithNoActiveWorkersSkips(t *testing.T) {
	runtime := &rolloutWorkersRuntime{nodeSet: rolloutWorkerTestNodeSet(true)}
	runtime.nodeSet.Status.Replicas = 0
	steps := &WorkerRollingUpdate{info: &framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime: runtime, kubectl: framework.NewKubectlClient(runtime)}
	require.ErrorIs(t, steps.selectNodeSet(t.Context()), godog.ErrSkip)
	assert.Empty(t, steps.power.names)
	assert.NotContains(t, strings.Join(runtime.calls, "\n"), "scontrol")
}

func rolloutSelectionTestNodeSet(name string, replicas int32) kubeobjects.NodeSet {
	nodeSet := rolloutWorkerTestNodeSet(false)
	nodeSet.Metadata.Name = name
	nodeSet.Spec.Replicas = replicas
	nodeSet.Status.Replicas = replicas
	return nodeSet
}

func rolloutSelectionTestSteps(nodeSets ...kubeobjects.NodeSet) (*WorkerRollingUpdate, *rolloutWorkersRuntime) {
	runtime := &rolloutWorkersRuntime{nodeSets: nodeSets, podsByNodeSet: make(map[string][]corev1.Pod)}
	for _, nodeSet := range nodeSets {
		for ordinal := range nodeSet.Spec.Replicas {
			name := fmt.Sprintf("%s-%d", nodeSet.Metadata.Name, ordinal)
			pod := readyWorkerPod(name, types.UID(name+"-uid"), nil)
			pod.ResourceVersion = "1"
			pod.Namespace = framework.SoperatorNamespace
			pod.Spec.NodeName = name + "-host"
			pod.Spec.Hostname = name
			pod.Status.StartTime = new(metav1.Now())
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "slurmd", ContainerID: "containerd://" + name}}
			runtime.podsByNodeSet[nodeSet.Metadata.Name] = append(runtime.podsByNodeSet[nodeSet.Metadata.Name], pod)
			runtime.nodes = append(runtime.nodes, corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name: pod.Spec.NodeName, UID: types.UID(pod.Spec.NodeName + "-uid"),
			}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}})
		}
	}
	kubectl := framework.NewKubectlClient(runtime)
	slurm := framework.NewSlurmClient(runtime)
	steps := NewWorkerRollingUpdate(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, slurm, kubectl,
		framework.NewWorkerSelector(kubectl, slurm, "soperator"))
	return steps, runtime
}

func TestSelectNodeSetSearchesForTwoActiveWorkers(t *testing.T) {
	steps, runtime := rolloutSelectionTestSteps(rolloutSelectionTestNodeSet("a-gpu", 1), rolloutSelectionTestNodeSet("b-gpu", 2))
	require.NoError(t, steps.selectNodeSet(t.Context()))
	assert.Equal(t, "b-gpu", steps.nodeSet.Metadata.Name)
	assert.Equal(t, []string{"b-gpu-0", "b-gpu-1"}, steps.workerNames)
	assert.NotContains(t, strings.Join(runtime.calls, "\n"), "slurm.nebius.ai/nodeset=a-gpu", "unsuitable targets need no pod discovery")
}

func TestSelectNodeSetRequiresConcurrentRolloutBudgetBeforeStartingJobs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		value     *intstr.IntOrString
		ephemeral bool
		wantSkip  bool
		wantError bool
	}{
		{name: "default permits only one", wantSkip: true},
		{name: "one unavailable", value: new(intstr.FromInt(1)), wantSkip: true},
		{name: "two unavailable", value: new(intstr.FromInt(2))},
		{name: "percentage allows both", value: new(intstr.FromString("100%"))},
		{name: "sparse active percentage permits only one", value: new(intstr.FromString("50%")), ephemeral: true, wantSkip: true},
		{name: "invalid budget fails", value: new(intstr.FromString("bad")), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodeSet := rolloutSelectionTestNodeSet("worker", 2)
			nodeSet.Spec.MaxUnavailable = tc.value
			steps, runtime := rolloutSelectionTestSteps(nodeSet)
			if tc.ephemeral {
				runtime.nodeSets = nil
				runtime.nodeSet = nodeSet
				runtime.nodeSet.Spec.Replicas = 10
				runtime.nodeSet.Spec.EphemeralNodes = new(true)
				runtime.active = []int32{0, 3}
			}
			err := steps.prepareRolloutWorkers(t.Context())
			if tc.wantSkip {
				require.ErrorIs(t, err, godog.ErrSkip)
			} else if tc.wantError {
				require.ErrorContains(t, err, "parse maxUnavailable")
				assert.NotErrorIs(t, err, godog.ErrSkip)
			} else {
				require.NoError(t, err)
				require.Len(t, runtime.submissions, 2)
				return
			}
			assert.Empty(t, runtime.submissions)
			assert.Empty(t, steps.power.names)
			assert.NotContains(t, strings.Join(runtime.calls, "\n"), "get pods", "reject the budget before worker discovery or suspension holds")
			assert.NotContains(t, strings.Join(runtime.calls, "\n"), "scontrol update")
		})
	}
}

func TestSelectNodeSetSearchesPastSingleWorkerBudget(t *testing.T) {
	limited := rolloutSelectionTestNodeSet("a-worker", 2)
	limited.Spec.MaxUnavailable = new(intstr.FromInt(1))
	steps, runtime := rolloutSelectionTestSteps(limited, rolloutSelectionTestNodeSet("b-worker", 2))
	require.NoError(t, steps.prepareRolloutWorkers(t.Context()))
	assert.Equal(t, "b-worker", steps.nodeSet.Metadata.Name)
	for _, submission := range runtime.submissions {
		assert.Contains(t, submission, "--nodelist='b-worker-")
	}
	assert.NotContains(t, strings.Join(runtime.calls, "\n"), "slurm.nebius.ai/nodeset=a-worker")
}

func TestSelectNodeSetSkipsOnlyUnsupportedCapacity(t *testing.T) {
	steps, runtime := rolloutSelectionTestSteps(rolloutSelectionTestNodeSet("a-gpu", 1), rolloutSelectionTestNodeSet("b-gpu", 1))
	require.ErrorIs(t, steps.selectNodeSet(t.Context()), godog.ErrSkip)
	assert.Empty(t, steps.nodeSet.Metadata.Name)
	assert.Empty(t, steps.power.names)
	assert.NotContains(t, strings.Join(runtime.calls, "\n"), "scontrol")

	runtime.nodeSets[0].Status.Phase = "Provisioning"
	err := steps.selectNodeSet(t.Context())
	require.ErrorContains(t, err, "require configured rollout NodeSet a-gpu to be healthy")
	assert.NotErrorIs(t, err, godog.ErrSkip, "configured but unhealthy resources must not be hidden by a skip")
}

func TestPrepareRolloutWorkersStartsTwoJobsWithoutMaintenanceSnapshots(t *testing.T) {
	steps, runtime := rolloutSelectionTestSteps(rolloutSelectionTestNodeSet("worker", 3))
	require.NoError(t, steps.prepareRolloutWorkers(t.Context()))
	require.Len(t, steps.targets, 2)
	require.Len(t, runtime.submissions, 2)
	assert.Len(t, steps.initialPods, 3, "the whole NodeSet remains part of final validation")
	for i, target := range steps.targets {
		worker := fmt.Sprintf("worker-%d", i)
		assert.Equal(t, worker, target.worker.Name)
		assert.Nil(t, target.replacement, "record provider identities only for a maintenance event")
		assert.Contains(t, runtime.submissions[i], "--nodelist="+framework.ShellQuote(worker))
	}
	assert.NotEqual(t, steps.targets[0].job.job.ID, steps.targets[1].job.job.ID)
	assert.NotEqual(t, steps.targets[0].job.releasePath, steps.targets[1].job.releasePath)
	calls := strings.Join(runtime.calls, "\n")
	assert.NotContains(t, calls, "get deployments", "maintenance tuning belongs to its scenario, not the shared Given")
	assert.NotContains(t, calls, "containerrecreaterequest", "container recovery is observed from worker state")
}

func TestPrepareRolloutWorkersSupportsCPUJobsAndRequiresSlurmAwareStrategy(t *testing.T) {
	for _, strategy := range []string{kubeobjects.NodeSetUpdateStrategySlurmAwareRollingUpdate, "rollingUpdate"} {
		t.Run(strategy, func(t *testing.T) {
			nodeSet := rolloutSelectionTestNodeSet("worker", 2)
			nodeSet.Spec.GPU.Enabled = false
			nodeSet.Spec.UpdateStrategy = strategy
			steps, runtime := rolloutSelectionTestSteps(nodeSet)
			err := steps.prepareRolloutWorkers(t.Context())
			if strategy != kubeobjects.NodeSetUpdateStrategySlurmAwareRollingUpdate {
				require.ErrorIs(t, err, godog.ErrSkip)
				assert.Empty(t, runtime.submissions, "an unsupported strategy must not start rollout jobs")
				return
			}
			require.NoError(t, err)
			require.Len(t, runtime.submissions, 2)
			for _, command := range runtime.submissions {
				assert.NotContains(t, command, "--gpus-per-node")
			}
		})
	}
}

func TestRolloutIdleAndExplicitSmokeJobsUseWorkerKind(t *testing.T) {
	for _, gpu := range []bool{false, true} {
		t.Run(fmt.Sprintf("GPU enabled=%t", gpu), func(t *testing.T) {
			nodeSet := rolloutSelectionTestNodeSet("worker", 2)
			nodeSet.Spec.GPU.Enabled = gpu
			steps, runtime := rolloutSelectionTestSteps(nodeSet)
			require.NoError(t, steps.selectNodeSet(t.Context()))
			require.NoError(t, steps.waitForWorkersIdle(t.Context()))
			for _, command := range runtime.calls {
				assert.NotContains(t, command, "srun ", "the IDLE check must not hide the explicit smoke step")
			}
			smokeStart := len(runtime.calls)
			require.NoError(t, steps.runRolloutSmokeJobs(t.Context()))
			var smokeCommands []string
			for _, command := range runtime.calls {
				if strings.HasPrefix(command, "timeout 180 srun ") {
					smokeCommands = append(smokeCommands, command)
				}
			}
			expected := []string{"timeout 180 srun -w 'worker-0' hostname", "timeout 180 srun -w 'worker-1' hostname"}
			if gpu {
				expected = []string{
					"timeout 180 srun -w 'worker-0' --gpus-per-node=1 nvidia-smi -L >/dev/null",
					"timeout 180 srun -w 'worker-1' --gpus-per-node=1 nvidia-smi -L >/dev/null",
				}
			}
			assert.Equal(t, expected, smokeCommands, "each active worker should accept exactly one smoke job")
			assert.Equal(t, expected, runtime.calls[smokeStart:], "the smoke step only runs jobs")
		})
	}
}

func TestEphemeralWorkerHoldPreservesPreexistingAndConcurrentExclusions(t *testing.T) {
	runtime := newRolloutPowerRuntime(t,
		powerConfigReply("worker-[0,2],other-0"),
		powerHostsReply("worker-[0,2],other-0", "worker-0\nworker-2\nother-0"),
		powerUpdateReply("+=", "worker-5"),
		powerConfigReply("worker-[0,2,5],other-0"),
		powerHostsReply("worker-[0,2,5],other-0", "worker-0\nworker-2\nworker-5\nother-0"),
		// An unrelated actor adds another exclusion before our cleanup.
		powerConfigReply("worker-[0,2,5],other-[0-1]"),
		powerHostsReply("worker-[0,2,5],other-[0-1]", "worker-0\nworker-2\nworker-5\nother-0\nother-1"),
		powerUpdateReply("-=", "worker-5"),
		powerConfigReply("worker-[0,2],other-[0-1]"),
		powerHostsReply("worker-[0,2],other-[0-1]", "worker-0\nworker-2\nother-0\nother-1"),
	)
	s := WorkerRollingUpdate{slurm: framework.NewSlurmClient(runtime), runtime: runtime}
	require.NoError(t, s.holdEphemeralWorkers(context.Background(), []string{"worker-5", "worker-0", "worker-5"}))
	assert.Equal(t, []string{"worker-0", "worker-5"}, s.power.names)
	assert.Equal(t, []string{"worker-5"}, s.power.added)
	s.cleanupEphemeralWorkerHold(context.Background())
	assert.Equal(t, workerRolloutPower{}, s.power)
	assert.Empty(t, runtime.logs)
	s.cleanupEphemeralWorkerHold(context.Background())
}

func TestEphemeralWorkerHoldCleansUncertainPartialMutation(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(fmt.Sprintf("applied=%t", applied), func(t *testing.T) {
			write := powerUpdateReply("+=", "worker-3,worker-7")
			write.err = errors.New("response lost")
			replies := []rolloutPowerReply{powerConfigReply("(null)"), write}
			if applied {
				// Only one addition is observed after an uncertain failure.
				replies = append(replies, powerConfigReply("worker-3"), powerHostsReply("worker-3", "worker-3"),
					powerUpdateReply("-=", "worker-3"), powerConfigReply("(null)"))
			} else {
				replies = append(replies, powerConfigReply("(null)"))
			}
			runtime := newRolloutPowerRuntime(t, replies...)
			s := WorkerRollingUpdate{slurm: framework.NewSlurmClient(runtime), runtime: runtime}
			require.ErrorContains(t, s.holdEphemeralWorkers(context.Background(), []string{"worker-3", "worker-7"}), "response lost")
			assert.Equal(t, []string{"worker-3", "worker-7"}, s.power.added)
			s.cleanupEphemeralWorkerHold(context.Background())
			assert.Empty(t, runtime.logs)
			assert.Equal(t, workerRolloutPower{}, s.power)
		})
	}
}

func TestEphemeralWorkerHoldCountedRangesNeverProveProtection(t *testing.T) {
	runtime := newRolloutPowerRuntime(t, powerConfigReply("worker-[0-9]:2"))
	s := WorkerRollingUpdate{slurm: framework.NewSlurmClient(runtime), runtime: runtime}
	require.ErrorContains(t, s.holdEphemeralWorkers(context.Background(), []string{"worker-3"}), "explicitly exclude these workers")
	assert.Empty(t, s.power.names)

	runtime = newRolloutPowerRuntime(t,
		powerConfigReply("worker-3,other-[0-9]:2"), powerHostsReply("worker-3", "worker-3"),
		powerConfigReply("worker-3,other-[0-9]:2"), powerHostsReply("worker-3", "worker-3"),
	)
	s = WorkerRollingUpdate{slurm: framework.NewSlurmClient(runtime), runtime: runtime}
	require.NoError(t, s.holdEphemeralWorkers(context.Background(), []string{"worker-3"}))
	assert.Empty(t, s.power.added)
	s.cleanupEphemeralWorkerHold(context.Background())
	assert.Empty(t, runtime.logs)
}

func TestEphemeralWorkerHoldLostProtectionFailsInvariant(t *testing.T) {
	runtime := newRolloutPowerRuntime(t, powerConfigReply("worker-3"), powerHostsReply("worker-3", "worker-3"))
	s := WorkerRollingUpdate{slurm: framework.NewSlurmClient(runtime), runtime: runtime, power: workerRolloutPower{names: []string{"worker-3", "worker-7"}}}
	err := s.checkEphemeralWorkerHold(context.Background())
	require.ErrorContains(t, err, "worker-7 lost automatic suspension protection")
	var invariant rolloutInvariantError
	assert.ErrorAs(t, err, &invariant)
}

func TestEphemeralWorkerHoldRejectsUnknownConfigAndInvalidNames(t *testing.T) {
	runtime := newRolloutPowerRuntime(t, rolloutPowerReply{command: "scontrol show config", output: "SuspendExcParts = none"})
	s := WorkerRollingUpdate{slurm: framework.NewSlurmClient(runtime), runtime: runtime}
	require.ErrorContains(t, s.holdEphemeralWorkers(context.Background(), []string{"worker-3"}), "missing SuspendExcNodes")
	assert.Empty(t, s.power.names)
	for _, names := range [][]string{nil, {"worker-[0-3]"}, {"worker-3;echo bad"}, {"worker-0,worker-3"}, {""}} {
		require.Error(t, s.holdEphemeralWorkers(context.Background(), names))
	}
}

func TestEphemeralWorkerHoldRejectsInvalidExpansion(t *testing.T) {
	for _, output := range []string{"", "worker-0 extra", "worker-[0-2]"} {
		runtime := newRolloutPowerRuntime(t, powerConfigReply("worker-[0-2]"), powerHostsReply("worker-[0-2]", output))
		s := WorkerRollingUpdate{slurm: framework.NewSlurmClient(runtime), runtime: runtime}
		require.ErrorContains(t, s.holdEphemeralWorkers(context.Background(), []string{"worker-0"}), "invalid node name")
		assert.Empty(t, s.power.names)
	}
}

func TestEphemeralWorkerHoldClassifiesReadFailures(t *testing.T) {
	for _, tt := range []struct {
		name      string
		reply     rolloutPowerReply
		invariant bool
	}{
		{name: "missing setting", reply: rolloutPowerReply{command: "scontrol show config", output: "SuspendExcParts = other"}, invariant: true},
		{name: "duplicate setting", reply: rolloutPowerReply{command: "scontrol show config", output: "SuspendExcNodes = a\nSuspendExcNodes = b"}, invariant: true},
		{name: "malformed hostlist", reply: powerConfigReply("worker-[0"), invariant: true},
		{name: "command failure", reply: rolloutPowerReply{command: "scontrol show config", err: errors.New("controller unavailable")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime := newRolloutPowerRuntime(t, tt.reply)
			s := WorkerRollingUpdate{runtime: runtime, slurm: framework.NewSlurmClient(runtime), power: workerRolloutPower{names: []string{"worker-0"}}}
			err := s.checkEphemeralWorkerHold(t.Context())
			require.Error(t, err)
			var invariant rolloutInvariantError
			assert.Equal(t, tt.invariant, errors.As(err, &invariant))
		})
	}
}

func TestEphemeralWorkerHoldCleanupRefusesNewCountedConfiguration(t *testing.T) {
	runtime := newRolloutPowerRuntime(t,
		powerConfigReply("worker-3,other-[0-9]:2"), powerHostsReply("worker-3", "worker-3"))
	s := WorkerRollingUpdate{slurm: framework.NewSlurmClient(runtime), runtime: runtime, power: workerRolloutPower{names: []string{"worker-3"}, added: []string{"worker-3"}}}
	s.cleanupEphemeralWorkerHold(context.Background())
	require.Len(t, runtime.logs, 1)
	assert.Contains(t, runtime.logs[0], "refusing an incremental removal")
	assert.Contains(t, runtime.logs[0], "worker-3")
	assert.Equal(t, workerRolloutPower{}, s.power)
}

func TestEphemeralWorkerHoldCleanupChecksRemovalAndResets(t *testing.T) {
	runtime := newRolloutPowerRuntime(t,
		powerConfigReply("worker-3"), powerHostsReply("worker-3", "worker-3"),
		powerUpdateReply("-=", "worker-3"),
		powerConfigReply("worker-3"), powerHostsReply("worker-3", "worker-3"))
	s := WorkerRollingUpdate{slurm: framework.NewSlurmClient(runtime), runtime: runtime, power: workerRolloutPower{names: []string{"worker-3"}, added: []string{"worker-3"}}}
	s.cleanupEphemeralWorkerHold(context.Background())
	require.Len(t, runtime.logs, 1)
	assert.Contains(t, runtime.logs[0], "remains after removal")
	assert.Equal(t, workerRolloutPower{}, s.power)
}

func newRolloutPowerRuntime(t *testing.T, replies ...rolloutPowerReply) *rolloutPowerRuntime {
	t.Helper()
	r := &rolloutPowerRuntime{t: t, replies: replies}
	t.Cleanup(func() { assert.Empty(t, r.replies, "unconsumed Slurm commands") })
	return r
}

func (r *rolloutPowerRuntime) Controller() framework.CommandScope { return rolloutPowerScope{r: r} }

func (r *rolloutPowerRuntime) Logf(format string, args ...any) {
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}

func (s rolloutPowerScope) Run(_ context.Context, command string) (string, error) {
	s.r.t.Helper()
	require.NotEmpty(s.r.t, s.r.replies, "unexpected Slurm command: %s", command)
	reply := s.r.replies[0]
	s.r.replies = s.r.replies[1:]
	require.Equal(s.r.t, reply.command, command)
	return reply.output, reply.err
}

func (s rolloutPowerScope) RunWithDefaultRetry(ctx context.Context, command string) (string, error) {
	return s.Run(ctx, command)
}

func powerConfigReply(value string) rolloutPowerReply {
	return rolloutPowerReply{command: "scontrol show config", output: "SuspendExcNodes = " + value}
}

func powerHostsReply(expression, names string) rolloutPowerReply {
	return rolloutPowerReply{command: "scontrol show hostnames " + framework.ShellQuote(expression), output: names}
}

func powerUpdateReply(operator, names string) rolloutPowerReply {
	return rolloutPowerReply{command: "scontrol update " + framework.ShellQuote("SuspendExcNodes"+operator+names)}
}
