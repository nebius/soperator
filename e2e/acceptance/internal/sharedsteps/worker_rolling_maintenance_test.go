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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

type maintenanceTestRuntime struct {
	framework.Runtime
	pods       map[string]corev1.Pod
	hosts      map[string]corev1.Node
	nodes      map[string]string
	calls      []string
	iteration  int
	waitLabels []string
	observe    func(*maintenanceTestRuntime)
	command    func(string) (string, error)
	kube       func(...string) (string, error)
	local      func(...string) (string, error)
}

func (r *maintenanceTestRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r.calls = append(r.calls, "kubectl "+strings.Join(args, " "))
		if r.kube != nil {
			return r.kube(args...)
		}
		if args[0] == "get" && args[1] == "deployments" {
			data, err := json.Marshal(appsv1.DeploymentList{Items: []appsv1.Deployment{readyChecksDeployment("15s")}})
			return string(data), err
		}
		if args[0] == "patch" {
			for name, pod := range r.pods {
				if pod.Spec.NodeName != args[2] {
					continue
				}
				state := r.nodes[name]
				if !strings.Contains(state, "+DRAIN") {
					state = strings.Replace(state, "State=ALLOCATED", "State=ALLOCATED+DRAIN", 1)
				}
				if index := strings.Index(state, " Reason="); index >= 0 {
					state = state[:index]
				}
				r.nodes[name] = state + " Reason=" + nodeReplacementDrainReason
			}
			return "", nil
		}
		if args[1] == "pod" {
			pod, ok := r.pods[args[2]]
			if !ok {
				return "", nil
			}
			data, err := json.Marshal(pod)
			return string(data), err
		}
		if args[1] == "node" {
			host, ok := r.hosts[args[2]]
			if !ok {
				return "", nil
			}
			data, err := json.Marshal(host)
			return string(data), err
		}
		if args[1] == "pods" {
			var pods []corev1.Pod
			for _, pod := range r.pods {
				pods = append(pods, pod)
			}
			data, err := json.Marshal(corev1.PodList{Items: pods})
			return string(data), err
		}
		return "", fmt.Errorf("unexpected Kubernetes command: %v", args)
	})
}

func (r *maintenanceTestRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r.calls = append(r.calls, command)
		if r.command != nil {
			return r.command(command)
		}
		switch {
		case strings.HasPrefix(command, "squeue "):
			return "RUNNING", nil
		case strings.HasPrefix(command, "sacct "):
			if strings.Contains(command, "'6'") {
				return "6|RUNNING|0:0|None|", nil
			}
			return "7|RUNNING|0:0|None|", nil
		case strings.HasPrefix(command, "scontrol -o show job "):
			return "Restarts=0", nil
		case strings.HasPrefix(command, "touch -- "):
			return "", nil
		case strings.HasPrefix(command, "scontrol show node "):
			name := strings.Trim(strings.TrimPrefix(command, "scontrol show node "), "'")
			return r.nodes[name], nil
		default:
			return "", fmt.Errorf("unexpected Slurm command: %s", command)
		}
	})
}

func (r *maintenanceTestRuntime) Controller() framework.CommandScope { return r.Jail() }
func (r *maintenanceTestRuntime) Local() framework.ArgsScope {
	return framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r.calls = append(r.calls, "local "+strings.Join(args, " "))
		if r.local != nil {
			return r.local(args...)
		}
		return "", fmt.Errorf("unexpected local command: %v", args)
	})
}
func (*maintenanceTestRuntime) Logf(string, ...any) {}
func (r *maintenanceTestRuntime) WaitFor(ctx context.Context, label string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	r.waitLabels = append(r.waitLabels, label)
	var last error
	for r.iteration = 1; r.iteration <= 3; r.iteration++ {
		if r.observe != nil {
			r.observe(r)
		}
		done, err := condition(ctx)
		last = err
		if done && err == nil {
			return nil
		}
	}
	return fmt.Errorf("%s remains pending: %w", label, last)
}

func maintenanceTestSteps(t *testing.T) (*WorkerRollingUpdate, *maintenanceTestRuntime) {
	t.Helper()
	runtime := &maintenanceTestRuntime{pods: make(map[string]corev1.Pod), hosts: make(map[string]corev1.Node), nodes: make(map[string]string)}
	steps := &WorkerRollingUpdate{
		runtime: runtime, kubectl: framework.NewKubectlClient(runtime), slurm: framework.NewSlurmClient(runtime),
		nodeSet:     kubeobjects.NodeSet{Metadata: kubeobjects.ObjectMeta{Name: "worker", Namespace: "soperator"}},
		initialPods: make(map[string]workerPodSnapshot), annotationValue: "updated",
	}
	for i, id := range []string{"6", "7"} {
		name := fmt.Sprintf("worker-%d", i)
		pod := readyWorkerPod(name, types.UID(name+"-original"), nil)
		pod.Namespace, pod.Spec.Hostname, pod.Spec.NodeName = "soperator", name, name+"-host"
		runtime.pods[name] = pod
		runtime.hosts[pod.Spec.NodeName] = corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: pod.Spec.NodeName, UID: types.UID(name + "-host-uid")}}
		runtime.nodes[name] = "NodeName=" + name + " State=ALLOCATED+DRAIN CPUAlloc=1 AllocMem=128 InstanceId=" + pod.Spec.NodeName + " Reason=" + nodeReplacementDrainReason
		worker := framework.WorkerInfo{Name: name}
		replacement := NewNodeReplacement(runtime, steps.slurm, nil)
		replacement.replacementWorker = worker
		replacement.original = framework.WorkerReplacementSnapshot{Namespace: pod.Namespace, WorkerName: name, PodName: name, PodUID: pod.UID, NodeName: pod.Spec.NodeName, NodeUID: types.UID(name + "-host-uid"), InstanceID: pod.Spec.NodeName}
		steps.targets = append(steps.targets, &workerRolloutTarget{worker: worker, replacement: replacement,
			job: workerRolloutJob{job: framework.SbatchJob{ID: id}, releasePath: "/scenario/" + name + ".release"}})
		steps.initialPods[name] = workerPodSnapshot{UID: pod.UID, NodeName: pod.Spec.NodeName}
		steps.workers = append(steps.workers, worker)
	}
	return steps, runtime
}

func TestMaintenanceEventOnlyTriggersTheSelectedNode(t *testing.T) {
	for _, worker := range []string{"first", "second"} {
		t.Run(worker, func(t *testing.T) {
			steps, runtime := maintenanceTestSteps(t)
			runtime.command = func(string) (string, error) {
				return "", errors.New("Slurm state is unavailable while triggering the event")
			}
			require.NoError(t, steps.triggerMaintenance(t.Context(), worker))
			target, err := steps.maintenanceTarget(worker)
			require.NoError(t, err)
			assert.True(t, target.replacement.maintenanceRequested)
			assert.Contains(t, strings.Join(runtime.calls, "\n"), "patch node "+target.replacement.original.NodeName)
			assert.Empty(t, runtime.waitLabels, "the drain is checked in the explicit Then step")
		})
	}
}

func TestMaintenanceEventCapturesCleanupIdentitiesOnlyWhenTriggered(t *testing.T) {
	for _, worker := range []string{"first", "second"} {
		t.Run(worker, func(t *testing.T) {
			steps, runtime := maintenanceTestSteps(t)
			target, err := steps.maintenanceTarget(worker)
			require.NoError(t, err)
			target.replacement = nil
			require.NoError(t, steps.triggerMaintenance(t.Context(), worker))
			require.NotNil(t, target.replacement)
			original := target.replacement.original
			assert.Equal(t, runtime.pods[target.worker.Name].UID, original.PodUID)
			assert.Equal(t, target.worker.Name+"-host", original.NodeName)
			assert.Equal(t, original.NodeName, original.InstanceID)
			assert.True(t, target.replacement.maintenanceRequested)
			calls := strings.Join(runtime.calls, "\n")
			assert.Less(t, strings.Index(calls, "scontrol show node "+framework.ShellQuote(target.worker.Name)), strings.Index(calls, "patch node "+original.NodeName))
			assert.Empty(t, runtime.waitLabels)
		})
	}
}

func TestWorkerDrainChecksOnlyTheSelectedWorkersSlurmState(t *testing.T) {
	for _, worker := range []string{"first", "second"} {
		for _, reason := range []string{"maintenance", "rolling update"} {
			t.Run(worker+" "+reason, func(t *testing.T) {
				steps, runtime := maintenanceTestSteps(t)
				target, err := steps.maintenanceTarget(worker)
				require.NoError(t, err)
				target.replacement = nil
				// The step must not require the original Pod or a particular reboot phase.
				runtime.pods, runtime.hosts = nil, nil
				drainReason := nodeReplacementDrainReason
				if reason == "rolling update" {
					drainReason = workerRollingUpdateDrainReason
				}
				runtime.nodes[target.worker.Name] = "NodeName=" + target.worker.Name + " State=IDLE+DRAIN+REBOOT_ISSUED Reason=" + drainReason
				require.NoError(t, steps.assertWorkerDrain(t.Context(), worker, reason))
				for _, call := range runtime.calls {
					assert.Equal(t, "scontrol show node "+framework.ShellQuote(target.worker.Name), call)
				}
			})
		}
	}
}

func TestWorkerDrainReasonMatching(t *testing.T) {
	for _, tt := range []struct {
		name   string
		reason string
		state  string
		actual string
		want   bool
	}{
		{name: "rolling update", reason: "rolling update", state: "ALLOCATED+DRAIN", actual: workerRollingUpdateDrainReason, want: true},
		{name: "rolling update with Slurm stamp", reason: "rolling update", state: "ALLOCATED+DRAIN", actual: "soperator rolling update [root@2026-10-14T09:41:40]", want: true},
		{name: "rolling update with details", reason: "rolling update", state: "ALLOCATED+DRAIN", actual: workerRollingUpdateDrainReason + " : reboot requested", want: true},
		{name: "maintenance", reason: "maintenance", state: "ALLOCATED+DRAIN", actual: nodeReplacementDrainReason, want: true},
		{name: "maintenance reboot", reason: "maintenance", state: "IDLE+DRAIN", actual: "[compute_maintenance] node reboot process [root@2026-10-14T09:41:40]", want: true},
		{name: "reason without drain", reason: "rolling update", state: "IDLE", actual: workerRollingUpdateDrainReason},
		{name: "different drain", reason: "rolling update", state: "IDLE+DRAIN", actual: nodeReplacementDrainReason},
		{name: "similar rolling reason", reason: "rolling update", state: "IDLE+DRAIN", actual: "soperator rolling updates"},
		{name: "embedded rolling reason", reason: "rolling update", state: "IDLE+DRAIN", actual: "manual " + workerRollingUpdateDrainReason},
		{name: "maintenance reason without drain", reason: "maintenance", state: "IDLE", actual: nodeReplacementDrainReason},
		{name: "different maintenance drain", reason: "maintenance", state: "IDLE+DRAIN", actual: "manual drain"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			steps, runtime := maintenanceTestSteps(t)
			runtime.nodes["worker-1"] = "NodeName=worker-1 State=" + tt.state + " Reason=" + tt.actual
			err := steps.assertWorkerDrain(t.Context(), "second", tt.reason)
			if tt.want {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestMaintenancePreparationRetainsRestorationStateAfterLostResponse(t *testing.T) {
	runtime, deployment, release, calls := checksIntervalFixture(t)
	original := append([]string(nil), deployment.Spec.Template.Spec.Containers[0].Args...)
	scope := runtime.kube
	lost := false
	runtime.kube = rolloutCleanupArgsScope{framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		out, err := scope.Run(ctx, args...)
		if args[0] == "patch" && args[1] == "deployment" && !lost {
			lost = true
			return "", errors.New("response lost")
		}
		return out, err
	})}
	steps := &WorkerRollingUpdate{runtime: runtime, kubectl: framework.NewKubectlClient(runtime)}
	for range 2 {
		steps.targets = append(steps.targets, &workerRolloutTarget{replacement: NewNodeReplacement(runtime, nil, nil)})
	}
	require.ErrorContains(t, steps.triggerMaintenance(t.Context(), "first"), "response lost")
	require.NotNil(t, steps.checksInterval)
	assert.NotContains(t, strings.Join(*calls, "\n"), "patch node ")
	require.NoError(t, steps.restoreMaintenanceChecksInterval(t.Context()))
	assert.Nil(t, steps.checksInterval)
	assert.Equal(t, original, deployment.Spec.Template.Spec.Containers[0].Args)
	assert.Nil(t, release.Spec.Suspend)
}

func TestNodeReplacementGuardsKeepSafetyFailuresFatal(t *testing.T) {
	for _, wait := range []string{"drain", "replacement"} {
		t.Run(wait, func(t *testing.T) {
			runtime := &rolloutPollRuntime{}
			replacement := &NodeReplacement{runtime: runtime}
			calls := 0
			guard := func(context.Context) error { calls++; return rolloutInvariant("protected job lost") }
			var err error
			if wait == "drain" {
				err = replacement.waitForMaintenanceDrain(t.Context(), guard)
			} else {
				err = replacement.waitForReplacement(t.Context(), time.Minute, guard)
			}
			var invariant rolloutInvariantError
			require.ErrorAs(t, err, &invariant)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestNodeReplacementRetainsEventOwnershipAfterLostPatchResponse(t *testing.T) {
	runtime := maintenancePatchRuntime{kubectl: framework.NewArgsScope(func(context.Context, ...string) (string, error) { return "", errors.New("response lost") })}
	steps := &NodeReplacement{runtime: runtime, original: framework.WorkerReplacementSnapshot{NodeName: "original-host"}}
	require.ErrorContains(t, steps.aMaintenanceEventIsTriggeredForThatNode(t.Context()), "response lost")
	assert.True(t, steps.maintenanceRequested, "cleanup must inspect whether the accepted event needs to finish")
}

func TestNodeReplacementCleanupDoesNotStartMaintenanceAfterRejectedPatch(t *testing.T) {
	var calls []string
	runtime := &maintenanceTestRuntime{kube: func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return `{"metadata":{"name":"original-host","uid":"original-uid"}}`, nil
	}}
	steps := &NodeReplacement{runtime: runtime, kubectl: framework.NewKubectlClient(runtime), maintenanceRequested: true, original: framework.WorkerReplacementSnapshot{NodeName: "original-host", NodeUID: "original-uid"}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	steps.CleanupAndReset(ctx)
	assert.Equal(t, []string{"get node original-host --ignore-not-found -o json"}, calls)
	assert.False(t, steps.maintenanceRequested)
	assert.Zero(t, steps.original)
}

func TestNodeReplacementGuardRetriesTransientObservation(t *testing.T) {
	steps, _ := maintenanceTestSteps(t)
	calls := 0
	guard := func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("temporary Kubernetes read error")
		}
		return nil
	}
	require.NoError(t, steps.targets[0].replacement.waitForMaintenanceDrain(t.Context(), guard))
	assert.Equal(t, 2, calls)
}

func readyChecksDeployment(interval string) appsv1.Deployment {
	return appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-checks", Namespace: framework.SoperatorSystemNamespace, UID: "checks-uid", ResourceVersion: "10", Generation: 2,
			Annotations: map[string]string{"meta.helm.sh/release-name": "custom-release", "meta.helm.sh/release-namespace": framework.SoperatorSystemNamespace}},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "manager",
			Args: []string{"--leader-elect", checksSlurmNodesIntervalFlag + "=" + interval, "--enable-node-replacement=true", "--unknown=value"}}}}}},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1},
	}
}

func TestMaintenanceChecksArgsPreserveUnrelatedFlags(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want []string
	}{
		{name: "equal form", args: []string{"--leader-elect", checksSlurmNodesIntervalFlag + "=3m", "--enable-node-replacement=true"},
			want: []string{"--leader-elect", checksSlurmNodesIntervalFlag + "=15s", "--enable-node-replacement=true"}},
		{name: "separate form", args: []string{checksSlurmNodesIntervalFlag, "3m", "--unknown=preserve"},
			want: []string{checksSlurmNodesIntervalFlag, "15s", "--unknown=preserve"}},
		{name: "default interval", args: []string{"--leader-elect"}, want: []string{"--leader-elect", checksSlurmNodesIntervalFlag + "=15s"}},
		{name: "absent args", want: []string{checksSlurmNodesIntervalFlag + "=15s"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := append([]string(nil), tt.args...)
			got, changed, err := maintenanceChecksArgs(tt.args)
			require.NoError(t, err)
			assert.True(t, changed)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, original, tt.args)
		})
	}
	for _, interval := range []string{"5s", "15s"} {
		args := []string{checksSlurmNodesIntervalFlag + "=" + interval}
		got, changed, err := maintenanceChecksArgs(args)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, args, got)
	}
	for _, args := range [][]string{{checksSlurmNodesIntervalFlag}, {checksSlurmNodesIntervalFlag + "=0s"}, {checksSlurmNodesIntervalFlag + "=bad"},
		{checksSlurmNodesIntervalFlag + "=3m", checksSlurmNodesIntervalFlag, "1m"}} {
		_, _, err := maintenanceChecksArgs(args)
		require.Error(t, err)
	}
}

func checksIntervalFixture(t *testing.T) (*rolloutCleanupRuntime, *appsv1.Deployment, *helmRelease, *[]string) {
	t.Helper()
	deployment := readyChecksDeployment("3m")
	release := helmRelease{Metadata: metav1.ObjectMeta{Name: "owner-checks", Namespace: "flux-custom", UID: "release-uid", ResourceVersion: "20",
		Annotations: map[string]string{helmDriftAnnotation: "original", "unrelated": "preserve"}},
		Status: helmReleaseStatusRef{Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}}}
	release.Spec.ReleaseName, release.Spec.TargetNamespace = "custom-release", framework.SoperatorSystemNamespace
	var calls []string
	runtime := &rolloutCleanupRuntime{}
	runtime.kube = rolloutCleanupArgsScope{framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		var value any
		switch {
		case args[0] == "get" && args[1] == "deployments":
			value = appsv1.DeploymentList{Items: []appsv1.Deployment{deployment}}
		case args[0] == "get" && args[1] == "deployment":
			value = deployment
		case args[0] == "get" && args[1] == helmReleaseResource && args[2] == "-A":
			value = helmReleaseList{Items: []helmRelease{release}}
		case args[0] == "get" && args[1] == helmReleaseResource:
			value = release
		case args[0] == "patch" && args[1] == "deployment":
			var patch []struct {
				Op    string          `json:"op"`
				Path  string          `json:"path"`
				Value json.RawMessage `json:"value"`
			}
			require.NoError(t, json.Unmarshal([]byte(args[len(args)-1]), &patch))
			require.Len(t, patch, 3)
			assert.Equal(t, "/metadata/uid", patch[0].Path)
			assert.Equal(t, "/spec/template/spec/containers/0", patch[1].Path)
			assert.Equal(t, "test", patch[1].Op)
			assert.Equal(t, "/spec/template/spec/containers/0/args", patch[2].Path)
			require.NoError(t, json.Unmarshal(patch[2].Value, &deployment.Spec.Template.Spec.Containers[0].Args))
			deployment.Generation++
			deployment.Status.ObservedGeneration = deployment.Generation
			return "", nil
		case args[0] == "patch" && args[1] == helmReleaseResource:
			var patch struct {
				Metadata struct {
					Annotations map[string]*string `json:"annotations"`
				} `json:"metadata"`
				Spec map[string]json.RawMessage `json:"spec"`
			}
			require.NoError(t, json.Unmarshal([]byte(args[len(args)-1]), &patch))
			if value, exists := patch.Spec["suspend"]; exists {
				require.NoError(t, json.Unmarshal(value, &release.Spec.Suspend))
			}
			for key, value := range patch.Metadata.Annotations {
				if value == nil {
					delete(release.Metadata.Annotations, key)
				} else {
					release.Metadata.Annotations[key] = *value
				}
			}
			return "", nil
		default:
			return "", fmt.Errorf("unexpected checks interval command: %v", args)
		}
		data, err := json.Marshal(value)
		return string(data), err
	})}
	return runtime, &deployment, &release, &calls
}

func TestMaintenanceChecksIntervalRestoresExactFluxAndArgsState(t *testing.T) {
	for _, suspend := range []string{"absent", "false", "true"} {
		t.Run(suspend, func(t *testing.T) {
			runtime, deployment, release, calls := checksIntervalFixture(t)
			if suspend != "absent" {
				release.Spec.Suspend = new(bool)
				*release.Spec.Suspend = suspend == "true"
			}
			if suspend == "absent" {
				delete(release.Metadata.Annotations, helmDriftAnnotation)
			}
			originalArgs := append([]string(nil), deployment.Spec.Template.Spec.Containers[0].Args...)
			originalSuspend := release.Spec.Suspend
			originalAnnotation, annotationExists := release.Metadata.Annotations[helmDriftAnnotation]
			state, err := prepareMaintenanceChecksInterval(t.Context(), runtime, framework.NewKubectlClient(runtime))
			require.NoError(t, err)
			require.NotNil(t, state)
			assert.True(t, *release.Spec.Suspend)
			assert.Equal(t, "disabled", release.Metadata.Annotations[helmDriftAnnotation])
			assert.Equal(t, []string{"--leader-elect", checksSlurmNodesIntervalFlag + "=15s", "--enable-node-replacement=true", "--unknown=value"}, deployment.Spec.Template.Spec.Containers[0].Args)
			assert.Less(t, strings.Index(strings.Join(*calls, "\n"), "patch "+helmReleaseResource), strings.Index(strings.Join(*calls, "\n"), "patch deployment"))
			require.NoError(t, state.restore(t.Context(), runtime, framework.NewKubectlClient(runtime)))
			assert.Equal(t, originalArgs, deployment.Spec.Template.Spec.Containers[0].Args)
			assert.Equal(t, originalSuspend, release.Spec.Suspend)
			actualAnnotation, actualExists := release.Metadata.Annotations[helmDriftAnnotation]
			assert.Equal(t, originalAnnotation, actualAnnotation)
			assert.Equal(t, annotationExists, actualExists)
			assert.Equal(t, "preserve", release.Metadata.Annotations["unrelated"])
			count := len(*calls)
			require.NoError(t, state.restore(t.Context(), runtime, framework.NewKubectlClient(runtime)))
			assert.Len(t, *calls, count)
		})
	}
}

func TestMaintenanceChecksIntervalCleansUpAfterLostPatchResponse(t *testing.T) {
	runtime, deployment, release, _ := checksIntervalFixture(t)
	originalArgs := append([]string(nil), deployment.Spec.Template.Spec.Containers[0].Args...)
	scope := runtime.kube
	lost := false
	runtime.kube = rolloutCleanupArgsScope{framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		out, err := scope.Run(ctx, args...)
		if args[0] == "patch" && args[1] == "deployment" && !lost {
			lost = true
			return "", errors.New("response lost")
		}
		return out, err
	})}
	state, err := prepareMaintenanceChecksInterval(t.Context(), runtime, framework.NewKubectlClient(runtime))
	require.ErrorContains(t, err, "response lost")
	require.NotNil(t, state)
	require.NoError(t, state.restore(t.Context(), runtime, framework.NewKubectlClient(runtime)))
	assert.Equal(t, originalArgs, deployment.Spec.Template.Spec.Containers[0].Args)
	assert.Nil(t, release.Spec.Suspend)
	assert.Equal(t, "original", release.Metadata.Annotations[helmDriftAnnotation])
}

func TestMaintenanceChecksIntervalPreservesExternalArgsAndResumesFlux(t *testing.T) {
	runtime, deployment, release, _ := checksIntervalFixture(t)
	state, err := prepareMaintenanceChecksInterval(t.Context(), runtime, framework.NewKubectlClient(runtime))
	require.NoError(t, err)
	deployment.Spec.Template.Spec.Containers[0].Args = []string{"--external-change=true"}
	require.ErrorContains(t, state.restore(t.Context(), runtime, framework.NewKubectlClient(runtime)), "arguments changed outside the scenario")
	assert.Equal(t, []string{"--external-change=true"}, deployment.Spec.Template.Spec.Containers[0].Args)
	assert.Nil(t, release.Spec.Suspend)
	assert.Equal(t, "original", release.Metadata.Annotations[helmDriftAnnotation])
}

func TestMaintenanceChecksIntervalAlreadyFastDoesNotHoldFlux(t *testing.T) {
	runtime, deployment, _, calls := checksIntervalFixture(t)
	deployment.Spec.Template.Spec.Containers[0].Args = []string{checksSlurmNodesIntervalFlag + "=10s"}
	state, err := prepareMaintenanceChecksInterval(t.Context(), runtime, framework.NewKubectlClient(runtime))
	require.NoError(t, err)
	assert.Nil(t, state)
	assert.Len(t, *calls, 1)
}

func TestMaintenanceChecksIntervalPreservesForeignDriftAnnotationAndResumesFlux(t *testing.T) {
	runtime, _, release, _ := checksIntervalFixture(t)
	state, err := prepareMaintenanceChecksInterval(t.Context(), runtime, framework.NewKubectlClient(runtime))
	require.NoError(t, err)
	release.Metadata.Annotations[helmDriftAnnotation] = "external-value"
	require.ErrorContains(t, state.restore(t.Context(), runtime, framework.NewKubectlClient(runtime)), "drift annotation changed outside the scenario")
	assert.Equal(t, "external-value", release.Metadata.Annotations[helmDriftAnnotation])
	assert.Nil(t, release.Spec.Suspend, "restore owned suspension even when another owned field changed externally")
}

func TestMaintenanceChecksIntervalRejectedHoldPreservesConcurrentChanges(t *testing.T) {
	runtime, deployment, release, calls := checksIntervalFixture(t)
	original := append([]string(nil), deployment.Spec.Template.Spec.Containers[0].Args...)
	scope := runtime.kube
	runtime.kube = rolloutCleanupArgsScope{framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		if args[0] == "patch" && args[1] == helmReleaseResource {
			release.Spec.Suspend = new(bool)
			release.Metadata.Annotations[helmDriftAnnotation] = "external-value"
			return "", errors.New("resource version conflict")
		}
		return scope.Run(ctx, args...)
	})}
	state, err := prepareMaintenanceChecksInterval(t.Context(), runtime, framework.NewKubectlClient(runtime))
	require.ErrorContains(t, err, "resource version conflict")
	require.NotNil(t, state)
	err = state.restore(t.Context(), runtime, framework.NewKubectlClient(runtime))
	require.ErrorContains(t, err, "suspension changed outside the scenario")
	require.ErrorContains(t, err, "drift annotation changed outside the scenario")
	assert.Equal(t, "external-value", release.Metadata.Annotations[helmDriftAnnotation])
	require.NotNil(t, release.Spec.Suspend)
	assert.False(t, *release.Spec.Suspend)
	assert.Equal(t, original, deployment.Spec.Template.Spec.Containers[0].Args)
	assert.NotContains(t, strings.Join(*calls, "\n"), "patch deployment")
}

func TestMaintenanceChecksIntervalRetryRestorationAfterLostResponse(t *testing.T) {
	runtime, _, release, _ := checksIntervalFixture(t)
	state, err := prepareMaintenanceChecksInterval(t.Context(), runtime, framework.NewKubectlClient(runtime))
	require.NoError(t, err)
	scope := runtime.kube
	lost := false
	runtime.kube = rolloutCleanupArgsScope{framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		out, err := scope.Run(ctx, args...)
		if args[0] == "patch" && args[1] == helmReleaseResource && !lost {
			lost = true
			return "", errors.New("restoration response lost")
		}
		return out, err
	})}
	require.ErrorContains(t, state.restore(t.Context(), runtime, framework.NewKubectlClient(runtime)), "restoration response lost")
	assert.Nil(t, release.Spec.Suspend)
	assert.Equal(t, "original", release.Metadata.Annotations[helmDriftAnnotation])
	require.NoError(t, state.restore(t.Context(), runtime, framework.NewKubectlClient(runtime)))
	assert.False(t, state.releaseOwned)
}

func TestMaintenanceChecksIntervalSupportsDirectHelmAndRejectsQueryErrors(t *testing.T) {
	for _, query := range []string{"direct Helm", "missing CRD", "forbidden"} {
		t.Run(query, func(t *testing.T) {
			runtime, deployment, _, _ := checksIntervalFixture(t)
			scope := runtime.kube
			runtime.kube = rolloutCleanupArgsScope{framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
				if args[0] == "get" && args[1] == helmReleaseResource {
					switch query {
					case "missing CRD":
						return "", errors.New("the server doesn't have a resource type helmreleases")
					case "forbidden":
						return "", errors.New("Forbidden")
					default:
						return `{"items":[]}`, nil
					}
				}
				return scope.Run(ctx, args...)
			})}
			state, err := prepareMaintenanceChecksInterval(t.Context(), runtime, framework.NewKubectlClient(runtime))
			if query == "forbidden" {
				require.ErrorContains(t, err, "Forbidden")
				assert.Contains(t, deployment.Spec.Template.Spec.Containers[0].Args, checksSlurmNodesIntervalFlag+"=3m")
				return
			}
			require.NoError(t, err)
			assert.Nil(t, state.release)
			assert.Contains(t, deployment.Spec.Template.Spec.Containers[0].Args, checksSlurmNodesIntervalFlag+"=15s")
			require.NoError(t, state.restore(t.Context(), runtime, framework.NewKubectlClient(runtime)))
			assert.Contains(t, deployment.Spec.Template.Spec.Containers[0].Args, checksSlurmNodesIntervalFlag+"=3m")
		})
	}
}

func TestMaintenanceChecksIntervalRejectsUnhealthyConfiguredResources(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*appsv1.Deployment, *helmRelease)
	}{
		{name: "unready Deployment", mutate: func(d *appsv1.Deployment, _ *helmRelease) { d.Status.ReadyReplicas = 0 }},
		{name: "unobserved Deployment", mutate: func(d *appsv1.Deployment, _ *helmRelease) { d.Generation++ }},
		{name: "missing manager", mutate: func(d *appsv1.Deployment, _ *helmRelease) { d.Spec.Template.Spec.Containers[0].Name = "other" }},
		{name: "unready HelmRelease", mutate: func(_ *appsv1.Deployment, r *helmRelease) { r.Status.Conditions[0].Status = metav1.ConditionFalse }},
		{name: "missing HelmRelease identity", mutate: func(_ *appsv1.Deployment, r *helmRelease) { r.Metadata.UID = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime, deployment, release, calls := checksIntervalFixture(t)
			tt.mutate(deployment, release)
			state, err := prepareMaintenanceChecksInterval(t.Context(), runtime, framework.NewKubectlClient(runtime))
			require.Error(t, err)
			assert.NotContains(t, strings.Join(*calls, "\n"), "patch ")
			require.NoError(t, state.restore(t.Context(), runtime, framework.NewKubectlClient(runtime)))
		})
	}
}
