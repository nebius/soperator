package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const workerMK8sAutoscalingLiveShape = `{"metadata":{"id":"group-1","resource_version":"12"},"spec":{"autoscaling":{"max_node_count":"2"},"template":{"max_pods":"32"}},"status":{"state":"RUNNING","target_node_count":"2","node_count":"2","ready_node_count":"2","outdated_node_count":"0","strategy":{"drain_timeout":"0s","max_surge":{"percent":"0"},"max_unavailable":{"percent":"50"}}}}`

type workerMK8sTestRuntime struct {
	framework.Runtime
	local      func(...string) (string, error)
	controller func(string) (string, error)
	kube       func(...string) (string, error)
	polls      int
	kubeReads  int
	logs       []string
}

func TestNextWorkerMK8sMaxPods(t *testing.T) {
	for _, tt := range []struct {
		name     string
		original *mk8sInteger
		want     mk8sInteger
		wantErr  bool
	}{
		{"omitted default", nil, 111, false},
		{"explicit default", new(mk8sInteger(0)), 111, false},
		{"configured default", new(mk8sInteger(110)), 111, false},
		{"requested decrease", new(mk8sInteger(32)), 31, false},
		{"next run toggles back", new(mk8sInteger(31)), 32, false},
		{"CIDR boundary", new(mk8sInteger(128)), 129, false},
		{"maximum", new(mk8sInteger(256)), 255, false},
		{"minimum", new(mk8sInteger(8)), 9, false},
		{"below minimum", new(mk8sInteger(7)), 0, true},
		{"negative", new(mk8sInteger(-1)), 0, true},
		{"invalid", new(mk8sInteger(257)), 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nextWorkerMK8sMaxPods(tt.original)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWorkerMK8sDecodeAndFingerprint(t *testing.T) {
	input := `{"metadata":{"id":"group-1","resource_version":"12"},"spec":{"fixed_node_count":"2","template":{"max_pods":110,"cloud_init_user_data":"private","resources":{"platform":"gpu"}}},"status":{"state":"RUNNING","target_node_count":"2","node_count":2,"ready_node_count":2,"outdated_node_count":0}}`
	group, err := decodeWorkerMK8sGroup(input)
	require.NoError(t, err)
	require.NotNil(t, group.Spec.Template.MaxPods)
	assert.Equal(t, mk8sInteger(110), *group.Spec.Template.MaxPods)
	assert.Equal(t, mk8sInteger(12), group.Metadata.ResourceVersion)
	require.NoError(t, workerMK8sGroupReady(group, 2))

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(input), &raw))
	template := raw["spec"].(map[string]any)["template"].(map[string]any)
	template["max_pods"] = 111
	encoded, err := json.Marshal(raw)
	require.NoError(t, err)
	changed, err := decodeWorkerMK8sGroup(string(encoded))
	require.NoError(t, err)
	assert.Equal(t, group.fingerprint, changed.fingerprint)

	template["cloud_init_user_data"] = "other-private"
	encoded, err = json.Marshal(raw)
	require.NoError(t, err)
	changed, err = decodeWorkerMK8sGroup(string(encoded))
	require.NoError(t, err)
	assert.NotEqual(t, group.fingerprint, changed.fingerprint)
	assert.NotContains(t, changed.fingerprint, "private")

	_, err = decodeWorkerMK8sGroup(`{"metadata":{"id":"group-1"},"spec":{"template":{}}}`)
	require.ErrorContains(t, err, "resource_version")
}

func TestWorkerMK8sUpdateArgsPreserveOtherFields(t *testing.T) {
	assert.Equal(t, []string{"nebius", "mk8s", "node-group", "update",
		"--id", "group-1", "--patch", "--resource-version", "42", "--async", "--format", "json",
		"--template-max-pods", "33"}, workerMK8sUpdateArgs("group-1", 42, 33))
	assert.False(t, sameWorkerMK8sMaxPods(nil, new(mk8sInteger(0))), "selection must detect a concurrent change from omission to an explicit value")
}

func TestWorkerMK8sOperationParsing(t *testing.T) {
	for _, input := range []string{`"operation-1"`, `{"id":"operation-1"}`, "operation-1\n"} {
		id, err := workerMK8sOperationID(input)
		require.NoError(t, err)
		assert.Equal(t, "operation-1", id)
	}
	_, err := workerMK8sOperationID(`{"request":{"secret":"private"}}`)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "private")
	id, err := workerMK8sOperationID("mk8soperation-e00abc123\n")
	require.NoError(t, err)
	assert.Equal(t, "mk8soperation-e00abc123", id)
	_, err = workerMK8sOperationID("request failed")
	require.Error(t, err)

	done, err := workerMK8sOperationCompleted("operation-1", `{"id":"operation-1"}`)
	require.NoError(t, err)
	assert.False(t, done, "missing status is an operation in progress")
	done, err = workerMK8sOperationCompleted("operation-1", `{"id":"operation-1","status":{}}`)
	require.NoError(t, err)
	assert.True(t, done, "present empty status means successful completion")
	_, err = workerMK8sOperationCompleted("operation-1", `{"id":"operation-1","status":{"code":8,"message":"private"}}`)
	require.ErrorContains(t, err, "status code 8")
	assert.NotContains(t, err.Error(), "private")
	var invariant rolloutInvariantError
	assert.True(t, errors.As(err, &invariant))
	_, err = workerMK8sOperationCompleted("operation-1", `{"id":"operation-2","status":{}}`)
	require.ErrorContains(t, err, "expected operation-1")
}

func TestWorkerMK8sDedicatedPods(t *testing.T) {
	hosts := map[string]types.UID{"host-0": "host-uid", "idle-host": "idle-host-uid"}
	workers := map[string]workerPodSnapshot{"worker-0": {UID: "worker-uid", NodeName: "host-0"}}
	worker := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "soperator", Name: "worker-0", UID: "worker-uid"},
		Spec:       corev1.PodSpec{NodeName: "host-0"},
	}
	daemon := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "daemon", OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Controller: new(true)}}},
		Spec:       corev1.PodSpec{NodeName: "host-0"},
	}
	require.NoError(t, workerMK8sDedicatedPods(hosts, workers, []corev1.Pod{worker, daemon}, 8))
	idleDaemon := daemon.DeepCopy()
	idleDaemon.Spec.NodeName = "idle-host"
	require.NoError(t, workerMK8sDedicatedPods(hosts, workers, []corev1.Pod{worker, daemon, *idleDaemon}, 8))
	idleWorkload := idleDaemon.DeepCopy()
	idleWorkload.OwnerReferences = nil
	err := workerMK8sDedicatedPods(hosts, workers, []corev1.Pod{worker, *idleWorkload}, 8)
	var topology workerRolloutPrerequisiteError
	require.ErrorAs(t, err, &topology, "idle hosts must also be free of unrelated workloads")
	unrelated := worker.DeepCopy()
	unrelated.Name = "other-worker"
	err = workerMK8sDedicatedPods(hosts, workers, []corev1.Pod{worker, daemon, *unrelated}, 8)
	require.ErrorAs(t, err, &topology)
	unrelated.Name = worker.Name
	unrelated.UID = "unexpected-uid"
	require.Error(t, workerMK8sDedicatedPods(hosts, workers, []corev1.Pod{*unrelated}, 8))
	err = workerMK8sDedicatedPods(hosts, workers, []corev1.Pod{worker, daemon}, 1)
	require.ErrorContains(t, err, "leaving no spare Pod slot")
	assert.False(t, errors.As(err, &topology), "insufficient configured capacity is a failure, not a topology skip")
	require.ErrorContains(t, workerMK8sDedicatedPods(hosts, workers, []corev1.Pod{worker, daemon}, 2), "leaving no spare Pod slot", "later tests still need Pod capacity")
	require.NoError(t, workerMK8sDedicatedPods(hosts, workers, []corev1.Pod{worker, daemon}, 3))
}

func readyWorkerMK8sHost(name, group string) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name + "-uid"), Labels: map[string]string{workerMK8sGroupLabel: group}},
		Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	}
}

func TestWorkerMK8sHostsIncludeIdleGroupMembers(t *testing.T) {
	workers := map[string]workerPodSnapshot{"worker-1": {NodeName: "host-1"}, "worker-5": {NodeName: "host-5"}}
	nodes := []corev1.Node{
		readyWorkerMK8sHost("host-1", "group-1"), readyWorkerMK8sHost("host-5", "group-1"),
		readyWorkerMK8sHost("idle-host", "group-1"), readyWorkerMK8sHost("unrelated-host", "group-2"),
	}
	group, hosts, err := workerMK8sHosts(workers, nodes)
	require.NoError(t, err)
	assert.Equal(t, "group-1", group)
	assert.Equal(t, map[string]types.UID{"host-1": "host-1-uid", "host-5": "host-5-uid", "idle-host": "idle-host-uid"}, hosts)

	for _, tt := range []struct {
		name   string
		mutate func(*corev1.Node)
	}{
		{"not ready", func(n *corev1.Node) { n.Status.Conditions = nil }},
		{"cordoned", func(n *corev1.Node) { n.Spec.Unschedulable = true }},
		{"terminating", func(n *corev1.Node) { n.DeletionTimestamp = new(metav1.Now()) }},
		{"missing UID", func(n *corev1.Node) { n.UID = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			changed := slices.Clone(nodes)
			changed[2] = *nodes[2].DeepCopy()
			tt.mutate(&changed[2])
			_, _, err := workerMK8sHosts(workers, changed)
			require.ErrorContains(t, err, "idle-host is not ready and schedulable")
			var topology workerRolloutPrerequisiteError
			assert.False(t, errors.As(err, &topology), "an unhealthy idle host must fail rather than skip")
		})
	}
	_, _, err = workerMK8sHosts(map[string]workerPodSnapshot{"worker-1": {NodeName: "absent"}}, nodes)
	require.ErrorContains(t, err, "absent")
	_, _, err = workerMK8sHosts(map[string]workerPodSnapshot{"worker-1": {NodeName: "host-1"}, "worker-5": {NodeName: "host-1"}}, nodes)
	require.ErrorContains(t, err, "multiple workers")
	var colocated workerRolloutPrerequisiteError
	require.ErrorAs(t, err, &colocated, "co-located workers are an unsupported optional topology")
	_, _, err = workerMK8sHosts(map[string]workerPodSnapshot{"worker-1": {NodeName: "host-1"}, "worker-5": {NodeName: "unrelated-host"}}, nodes)
	var topology workerRolloutPrerequisiteError
	require.ErrorAs(t, err, &topology)
}

func TestSelectMK8sNodeSetSearchesForDedicatedHosts(t *testing.T) {
	steps, runtime := rolloutSelectionTestSteps(rolloutSelectionTestNodeSet("a-gpu", 2), rolloutSelectionTestNodeSet("b-gpu", 2))
	first := runtime.podsByNodeSet["a-gpu"]
	first[1].Spec.NodeName = first[0].Spec.NodeName
	runtime.nodes = append(runtime.nodes, readyWorkerMK8sHost(first[0].Spec.NodeName, "group-a"))
	for _, pod := range runtime.podsByNodeSet["b-gpu"] {
		runtime.nodes = append(runtime.nodes, readyWorkerMK8sHost(pod.Spec.NodeName, "group-1"))
	}
	reads := 0
	runtime.local = func(args ...string) (string, error) {
		assert.Equal(t, []string{"nebius", "mk8s", "node-group", "get", "--id", "group-1", "--format", "json"}, args)
		reads++
		return workerMK8sAutoscalingLiveShape, nil
	}
	require.NoError(t, steps.selectNodeSet(t.Context()))
	require.NoError(t, steps.selectMK8sGroup(t.Context()))
	assert.Equal(t, "b-gpu", steps.nodeSet.Metadata.Name)
	assert.Equal(t, 1, reads)
	assert.Equal(t, types.UID("b-gpu-0-uid"), steps.initialPods["b-gpu-0"].UID)
	assert.Empty(t, steps.power.added)
}

func TestSelectMK8sNodeSetSkipsOnlyUnsupportedTopology(t *testing.T) {
	steps, runtime := rolloutSelectionTestSteps(rolloutSelectionTestNodeSet("a-gpu", 2), rolloutSelectionTestNodeSet("b-gpu", 2))
	for _, nodeSet := range runtime.nodeSets {
		pods := runtime.podsByNodeSet[nodeSet.Metadata.Name]
		pods[1].Spec.NodeName = pods[0].Spec.NodeName
		runtime.nodes = append(runtime.nodes, readyWorkerMK8sHost(pods[0].Spec.NodeName, "group-1"))
	}
	require.ErrorIs(t, steps.selectNodeSet(t.Context()), godog.ErrSkip)
	assert.Empty(t, steps.nodeSet.Metadata.Name)
	assert.Empty(t, steps.mk8s.groupID)
	assert.Empty(t, steps.power.names)
	assert.NotContains(t, strings.Join(runtime.calls, "\n"), "scontrol update")

	runtime.podsByNodeSet["a-gpu"][1].Spec.NodeName = "absent-host"
	require.NoError(t, steps.selectNodeSet(t.Context()))
	err := steps.selectMK8sGroup(t.Context())
	require.ErrorContains(t, err, "host absent-host is absent")
	assert.NotErrorIs(t, err, godog.ErrSkip, "missing configured hosts are a preflight failure")
}

func TestWorkerMK8sStrategyAndConvergence(t *testing.T) {
	surge, err := workerMK8sLimitCount(workerMK8sLimit{Percent: new(mk8sInteger(1))}, 2, true)
	require.NoError(t, err)
	assert.Equal(t, 1, surge)
	unavailable, err := workerMK8sLimitCount(workerMK8sLimit{Percent: new(mk8sInteger(50))}, 3, false)
	require.NoError(t, err)
	assert.Equal(t, 1, unavailable)
	_, err = workerMK8sLimitCount(workerMK8sLimit{}, 2, false)
	require.Error(t, err)

	group, err := decodeWorkerMK8sGroup(`{"metadata":{"id":"group","resource_version":1},"spec":{"fixed_node_count":1,"template":{}},"status":{"state":"RUNNING","target_node_count":1,"node_count":1,"ready_node_count":1}}`)
	require.NoError(t, err)
	require.NoError(t, workerMK8sGroupReady(group, 1))
	group.Status.Reconciling = true
	require.Error(t, workerMK8sGroupReady(group, 1))
	group.Status.Reconciling = false
	group.Status.Outdated = 1
	require.Error(t, workerMK8sGroupReady(group, 1))
	group.Status.Outdated = 0
	group.Status.State = "running"
	require.NoError(t, workerMK8sGroupReady(group, 1))
}

func (r *workerMK8sTestRuntime) Local() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) { return r.local(args...) })
}

func (r *workerMK8sTestRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		r.kubeReads++
		if slices.Contains(args, "nodeset") {
			data, err := json.Marshal(kubeobjects.NodeSet{
				Metadata: kubeobjects.ObjectMeta{Name: "worker", Namespace: framework.SoperatorNamespace},
				Spec:     kubeobjects.NodeSetSpec{Replicas: 1},
				Status:   kubeobjects.NodeSetStatus{Replicas: 1, Phase: kubeobjects.NodeSetPhaseReady},
			})
			return string(data), err
		}
		if r.kube != nil {
			return r.kube(args...)
		}
		if slices.Contains(args, "nodes") {
			data, err := json.Marshal(corev1.NodeList{Items: []corev1.Node{readyWorkerMK8sHost("replacement-host", "group-1")}})
			return string(data), err
		}
		pod := readyWorkerPod("worker-0", "replacement-pod", nil)
		pod.Spec.NodeName = "replacement-host"
		data, err := json.Marshal(corev1.PodList{Items: []corev1.Pod{pod}})
		return string(data), err
	})
}

func (r *workerMK8sTestRuntime) Controller() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		if r.controller != nil {
			return r.controller(command)
		}
		return "NodeName=worker-0 State=IDLE InstanceId=replacement-instance", nil
	})
}

func (r *workerMK8sTestRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	var lastErr error
	for range 5 {
		r.polls++
		done, err := condition(ctx)
		if done && err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("test polling budget exhausted: %w", lastErr)
}

func (r *workerMK8sTestRuntime) Logf(format string, args ...any) {
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}

func workerMK8sTestGroup(version int, maxPods *mk8sInteger) string {
	template := map[string]any{"resources": map[string]any{"platform": "gpu"}}
	if maxPods != nil {
		template["max_pods"] = int64(*maxPods)
	}
	data, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{"id": "group-1", "resource_version": version},
		"spec":     map[string]any{"fixed_node_count": 1, "template": template},
		"status":   map[string]any{"state": "RUNNING", "target_node_count": 1, "node_count": 1, "ready_node_count": 1},
	})
	return string(data)
}

func workerMK8sTestSteps(t *testing.T, runtime *workerMK8sTestRuntime, original *mk8sInteger) *WorkerRollingUpdate {
	t.Helper()
	group, err := decodeWorkerMK8sGroup(workerMK8sTestGroup(1, original))
	require.NoError(t, err)
	return &WorkerRollingUpdate{
		runtime:     runtime,
		kubectl:     framework.NewKubectlClient(runtime),
		slurm:       framework.NewSlurmClient(runtime),
		nodeSet:     kubeobjects.NodeSet{Metadata: kubeobjects.ObjectMeta{Name: "worker", Namespace: framework.SoperatorNamespace}},
		workerNames: []string{"worker-0"},
		workers:     []framework.WorkerInfo{{Name: "worker-0"}},
		initialPods: map[string]workerPodSnapshot{"worker-0": {UID: "original-pod", NodeName: "original-host"}},
		mk8s: workerRolloutMK8s{
			groupID: "group-1", originalMaxPods: original, changedMaxPods: 111,
			specFingerprint: group.fingerprint,
		},
	}
}

func TestWorkerMK8sSparseWorkersUseWholeGroupForSelection(t *testing.T) {
	groupJSON := `{"metadata":{"id":"group-1","resource_version":1},"spec":{"fixed_node_count":2,"template":{"max_pods":110}},"status":{"state":"RUNNING","target_node_count":2,"node_count":2,"ready_node_count":2,"strategy":{"drain_timeout":"0s","max_surge":{"count":0},"max_unavailable":{"percent":50}}}}`
	nodes := []corev1.Node{readyWorkerMK8sHost("host-5", "group-1"), readyWorkerMK8sHost("idle-host", "group-1")}
	pod := readyWorkerPod("worker-5", "original-pod", nil)
	pod.Namespace = "soperator"
	pod.Spec.NodeName = "host-5"
	runtime := &workerMK8sTestRuntime{
		local: func(...string) (string, error) { return groupJSON, nil },
		kube: func(args ...string) (string, error) {
			var object any = corev1.PodList{Items: []corev1.Pod{pod}}
			if slices.Contains(args, "nodes") {
				object = corev1.NodeList{Items: nodes}
			}
			data, err := json.Marshal(object)
			return string(data), err
		},
	}
	steps := &WorkerRollingUpdate{
		runtime: runtime, kubectl: framework.NewKubectlClient(runtime),
		nodeSet:     kubeobjects.NodeSet{Metadata: kubeobjects.ObjectMeta{Name: "worker", Namespace: framework.SoperatorNamespace}},
		workers:     []framework.WorkerInfo{{Name: "worker-5"}},
		initialPods: map[string]workerPodSnapshot{"worker-5": {UID: pod.UID, NodeName: pod.Spec.NodeName}},
	}
	require.NoError(t, steps.selectMK8sGroup(t.Context()), "50%% unavailable is one of two hosts, not zero of one active worker")
	assert.Equal(t, "group-1", steps.mk8s.groupID)
	assert.Equal(t, &workerMK8sLimit{Count: new(mk8sInteger(2))}, steps.mk8s.changedMaxUnavailable)
}

func TestWorkerMK8sCleanupWithLostOperationIDAllowsPartialForwardRecovery(t *testing.T) {
	runtime := &workerMK8sTestRuntime{local: func(...string) (string, error) {
		return workerMK8sTestGroup(2, new(mk8sInteger(111))), nil
	}}
	steps := workerMK8sTestSteps(t, runtime, nil)
	runtime.kube = func(...string) (string, error) {
		data, err := json.Marshal(corev1.NodeList{Items: []corev1.Node{readyWorkerMK8sHost("original-host", "group-1")}})
		return string(data), err
	}
	steps.mk8s.requestedMaxPods = new(mk8sInteger(111))
	host := readyWorkerMK8sHost("original-host", "group-1")
	require.NoError(t, steps.mk8sCleanupReady(t.Context(), 1, []corev1.Node{host}),
		"healthy untouched hosts must not block undoing a partial forward rollout")
}

func TestWorkerMK8sRejectsConcurrentConfiguration(t *testing.T) {
	for _, duringCleanup := range []bool{false, true} {
		for _, change := range []string{"max_pods", "max_unavailable", "other spec"} {
			t.Run(fmt.Sprintf("during cleanup=%t/%s", duringCleanup, change), func(t *testing.T) {
				runtime := &workerMK8sTestRuntime{}
				steps := workerMK8sTestSteps(t, runtime, nil)
				mutations := 0
				runtime.local = func(args ...string) (string, error) {
					if slices.Contains(args, "update") {
						mutations++
						return "", fmt.Errorf("unexpected mutation")
					}
					current := workerMK8sTestGroup(4, new(mk8sInteger(112)))
					if change == "max_unavailable" {
						var maxPods *mk8sInteger
						if duringCleanup {
							maxPods = new(mk8sInteger(111))
						}
						current = strings.Replace(workerMK8sTestGroup(4, maxPods), `"spec":{`, `"spec":{"strategy":{"max_unavailable":{"count":3}},`, 1)
					}
					if change == "other spec" {
						var maxPods *mk8sInteger
						if duringCleanup {
							maxPods = new(mk8sInteger(111))
						}
						current = strings.Replace(workerMK8sTestGroup(4, maxPods), `"gpu"`, `"other-gpu"`, 1)
					}
					return current, nil
				}
				if duringCleanup {
					steps.mk8s.requestedMaxPods = new(mk8sInteger(111))
					err := steps.restoreMK8sGroup(t.Context())
					require.ErrorContains(t, err, "configuration changed externally")
					var invariant rolloutInvariantError
					assert.ErrorAs(t, err, &invariant)
				} else {
					require.ErrorContains(t, steps.changeMK8sGroup(t.Context()), "configuration changed after selection")
				}
				assert.Zero(t, mutations)
			})
		}
	}
}

func TestWorkerMK8sCleanupWaitsForOperation(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprintf("retry=%t", retry), func(t *testing.T) {
			runtime := &workerMK8sTestRuntime{}
			steps := workerMK8sTestSteps(t, runtime, nil)
			steps.mk8s.operationID = "operation-1"
			steps.mk8s.requestedMaxPods = new(mk8sInteger(111))
			pendingReads := 1
			if retry {
				pendingReads = 6
			}
			operationReads := 0
			runtime.local = func(args ...string) (string, error) {
				if slices.Contains(args, "update") {
					t.Fatal("waiting for provider convergence must not submit another update")
				}
				if slices.Contains(args, "operation") {
					operationReads++
					if operationReads <= pendingReads {
						return `{"id":"operation-1"}`, nil
					}
					return `{"id":"operation-1","status":{}}`, nil
				}
				return workerMK8sTestGroup(2, new(mk8sInteger(111))), nil
			}
			if retry {
				require.ErrorContains(t, steps.waitForMK8sCleanupReady(t.Context()), "polling budget exhausted")
			}
			require.NoError(t, steps.waitForMK8sCleanupReady(t.Context()))
			assert.Equal(t, pendingReads+1, operationReads)
			assert.Equal(t, "operation-1", steps.mk8s.operationID)
		})
	}
}

func TestWorkerMK8sAutoscalingConvergence(t *testing.T) {
	for _, tt := range []struct {
		name           string
		minimumWorkers int
		mutate         func(*workerMK8sGroup)
		wantErr        bool
	}{
		{name: "omitted minimum defaults to zero", minimumWorkers: 2},
		{name: "idle hosts may scale down", minimumWorkers: 1, mutate: func(g *workerMK8sGroup) {
			g.Status.Target, g.Status.Count, g.Status.Ready = 1, 1, 1
		}},
		{name: "active workers still require capacity", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) {
			g.Status.Target, g.Status.Count, g.Status.Ready = 1, 1, 1
		}},
		{name: "below configured minimum", minimumWorkers: 1, wantErr: true, mutate: func(g *workerMK8sGroup) {
			g.Spec.Autoscaling.MinNodeCount = 2
			g.Status.Target, g.Status.Count, g.Status.Ready = 1, 1, 1
		}},
		{name: "above configured maximum", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) {
			g.Status.Target, g.Status.Count, g.Status.Ready = 3, 3, 3
		}},
		{name: "negative minimum", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) { g.Spec.Autoscaling.MinNodeCount = -1 }},
		{name: "inverted bounds", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) { g.Spec.Autoscaling.MinNodeCount = 3 }},
		{name: "negative maximum", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) { g.Spec.Autoscaling.MaxNodeCount = -1 }},
		{name: "missing maximum cannot accommodate workers", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) { g.Spec.Autoscaling.MaxNodeCount = 0 }},
		{name: "mutually exclusive modes", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) { g.Spec.FixedNodeCount = new(mk8sInteger(2)) }},
		{name: "missing sizing mode", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) { g.Spec.Autoscaling = nil }},
		{name: "node count not converged", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) { g.Status.Count = 1 }},
		{name: "readiness not converged", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) { g.Status.Ready = 1 }},
		{name: "reconciling", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) { g.Status.Reconciling = true }},
		{name: "old configuration remains", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) { g.Status.Outdated = 1 }},
		{name: "not running", minimumWorkers: 2, wantErr: true, mutate: func(g *workerMK8sGroup) { g.Status.State = "PROVISIONING" }},
		{name: "fixed group allows idle hosts", minimumWorkers: 1, mutate: func(g *workerMK8sGroup) {
			g.Spec.Autoscaling = nil
			g.Spec.FixedNodeCount = new(mk8sInteger(2))
		}},
		{name: "fixed group must retain configured count", minimumWorkers: 1, wantErr: true, mutate: func(g *workerMK8sGroup) {
			g.Spec.Autoscaling = nil
			g.Spec.FixedNodeCount = new(mk8sInteger(2))
			g.Status.Target, g.Status.Count, g.Status.Ready = 1, 1, 1
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			group, err := decodeWorkerMK8sGroup(workerMK8sAutoscalingLiveShape)
			require.NoError(t, err)
			require.NotNil(t, group.Spec.Autoscaling)
			assert.Zero(t, group.Spec.Autoscaling.MinNodeCount)
			assert.Equal(t, mk8sInteger(2), group.Spec.Autoscaling.MaxNodeCount)
			if tt.mutate != nil {
				tt.mutate(&group)
			}
			err = workerMK8sGroupReady(group, tt.minimumWorkers)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWorkerMK8sAutoscalingGroupSelection(t *testing.T) {
	pods := []corev1.Pod{readyWorkerPod("worker-0", "pod-0", nil), readyWorkerPod("worker-1", "pod-1", nil)}
	nodes := []corev1.Node{readyWorkerMK8sHost("host-0", "group-1"), readyWorkerMK8sHost("host-1", "group-1")}
	workers := make([]framework.WorkerInfo, len(pods))
	snapshots := make(map[string]workerPodSnapshot, len(pods))
	for i := range pods {
		pods[i].Namespace = framework.SoperatorNamespace
		pods[i].Spec.NodeName = nodes[i].Name
		snapshots[pods[i].Name] = workerPodSnapshot{UID: pods[i].UID, NodeName: nodes[i].Name}
		workers[i] = framework.WorkerInfo{Name: pods[i].Name, SlurmNode: framework.SlurmNodeInfo{InstanceID: fmt.Sprintf("instance-%d", i)}}
	}
	runtime := &workerMK8sTestRuntime{
		local: func(...string) (string, error) { return workerMK8sAutoscalingLiveShape, nil },
		kube: func(args ...string) (string, error) {
			var object any = corev1.PodList{Items: pods}
			if slices.Contains(args, "nodes") {
				object = corev1.NodeList{Items: nodes}
			}
			data, err := json.Marshal(object)
			return string(data), err
		},
	}
	steps := &WorkerRollingUpdate{
		runtime: runtime, kubectl: framework.NewKubectlClient(runtime), workers: workers, initialPods: snapshots,
	}
	require.NoError(t, steps.selectMK8sGroup(t.Context()), "an autoscaling group with two healthy active workers must run, not skip")
	assert.Equal(t, mk8sInteger(31), steps.mk8s.changedMaxPods)

	// A status reporting two nodes cannot certify a Kubernetes snapshot containing
	// an additional group member, even though all three are individually ready.
	nodes = append(nodes, readyWorkerMK8sHost("unexpected-host", "group-1"))
	require.Error(t, steps.selectMK8sGroup(t.Context()))
}

func workerMK8sAutoscalingTestGroup(t *testing.T, version, target int, maxPods mk8sInteger) string {
	t.Helper()
	var group map[string]any
	require.NoError(t, json.Unmarshal([]byte(workerMK8sAutoscalingLiveShape), &group))
	group["metadata"].(map[string]any)["resource_version"] = version
	spec := group["spec"].(map[string]any)
	spec["autoscaling"].(map[string]any)["max_node_count"] = "3"
	spec["template"].(map[string]any)["max_pods"] = maxPods
	status := group["status"].(map[string]any)
	status["target_node_count"], status["node_count"], status["ready_node_count"] = target, target, target
	status["strategy"].(map[string]any)["max_unavailable"] = map[string]any{"count": 1}
	data, err := json.Marshal(group)
	require.NoError(t, err)
	return string(data)
}

func TestWorkerMK8sAutoscalingUpdatePreservesScaling(t *testing.T) {
	pod := readyWorkerPod("worker-0", "original-pod", nil)
	pod.Namespace, pod.Spec.NodeName = framework.SoperatorNamespace, "original-host"
	nodes := []corev1.Node{readyWorkerMK8sHost("original-host", "group-1"), readyWorkerMK8sHost("idle-host", "group-1")}
	target, version, updates := 2, 1, 0
	maxPods := mk8sInteger(32)
	runtime := &workerMK8sTestRuntime{}
	runtime.kube = func(args ...string) (string, error) {
		pods := []corev1.Pod{pod}
		var object any = corev1.PodList{Items: pods}
		if slices.Contains(args, "nodes") {
			object = corev1.NodeList{Items: nodes}
		}
		data, err := json.Marshal(object)
		return string(data), err
	}
	runtime.local = func(args ...string) (string, error) {
		switch {
		case slices.Contains(args, "update"):
			updates++
			wanted := mk8sInteger(31)
			require.Equal(t, appendWorkerMK8sLimitArgs(workerMK8sUpdateArgs("group-1", mk8sInteger(version), wanted), &workerMK8sLimit{Count: new(mk8sInteger(2))}), args,
				"the update changes Pod capacity and the rollout budget, preserving autoscaling bounds and mode")
			maxPods, version = wanted, version+1
			return fmt.Sprintf("operation-%d", updates), nil
		case slices.Contains(args, "operation"):
			return fmt.Sprintf(`{"id":"operation-%d","status":{}}`, updates), nil
		default:
			output := workerMK8sAutoscalingTestGroup(t, version, target, maxPods)
			if updates != 0 {
				output = strings.Replace(output, `"spec":{`, `"spec":{"strategy":{"max_unavailable":{"count":2}},`, 1)
				output = strings.Replace(output, `"max_unavailable":{"count":1}`, `"max_unavailable":{"count":2}`, 1)
			}
			return output, nil
		}
	}
	steps := &WorkerRollingUpdate{
		runtime: runtime, kubectl: framework.NewKubectlClient(runtime), slurm: framework.NewSlurmClient(runtime),
		nodeSet:     kubeobjects.NodeSet{Metadata: kubeobjects.ObjectMeta{Name: "worker", Namespace: framework.SoperatorNamespace}},
		workers:     []framework.WorkerInfo{{Name: "worker-0"}},
		initialPods: map[string]workerPodSnapshot{"worker-0": {UID: pod.UID, NodeName: pod.Spec.NodeName}},
	}
	require.NoError(t, steps.selectMK8sGroup(t.Context()))
	originalFingerprint := steps.mk8s.specFingerprint
	require.NoError(t, steps.changeMK8sGroup(t.Context()))

	require.NoError(t, steps.mk8sCleanupReady(t.Context(), 1, nodes))
	assert.Equal(t, 1, updates)
	assert.Equal(t, mk8sInteger(31), maxPods)
	group, err := decodeWorkerMK8sGroup(workerMK8sAutoscalingTestGroup(t, version, target, maxPods))
	require.NoError(t, err)
	assert.Equal(t, originalFingerprint, group.fingerprint)
	assert.Nil(t, group.Spec.FixedNodeCount)
	assert.Equal(t, mk8sInteger(3), group.Spec.Autoscaling.MaxNodeCount)
	assert.Zero(t, group.Spec.Autoscaling.MinNodeCount)
}

func TestWorkerMK8sAutoscalingBoundsRemainProtectedByFingerprint(t *testing.T) {
	group, err := decodeWorkerMK8sGroup(workerMK8sAutoscalingLiveShape)
	require.NoError(t, err)
	changed, err := decodeWorkerMK8sGroup(strings.Replace(workerMK8sAutoscalingLiveShape, `"max_node_count":"2"`, `"max_node_count":"3"`, 1))
	require.NoError(t, err)
	assert.NotEqual(t, group.fingerprint, changed.fingerprint)

	runtime := &workerMK8sTestRuntime{}
	steps := workerMK8sTestSteps(t, runtime, new(mk8sInteger(32)))
	steps.mk8s.specFingerprint, steps.mk8s.changedMaxPods = group.fingerprint, 33
	runtime.local = func(args ...string) (string, error) {
		if slices.Contains(args, "update") {
			t.Fatal("rollout must not overwrite a concurrent autoscaling change")
		}
		return strings.Replace(workerMK8sAutoscalingLiveShape, `"max_node_count":"2"`, `"max_node_count":"3"`, 1), nil
	}
	require.ErrorContains(t, steps.changeMK8sGroup(t.Context()), "configuration changed after selection")
}

func TestWorkerMK8sRestoresPodCapacityAndOriginalBudget(t *testing.T) {
	for _, tc := range []struct {
		name           string
		maxPods        *mk8sInteger
		budget         *workerMK8sLimit
		responseLost   int
		forwardFailure bool
		absentStrategy bool
		rejected       bool
		wantUpdates    int
	}{
		{name: "count", maxPods: new(mk8sInteger(32)), budget: &workerMK8sLimit{Count: new(mk8sInteger(1))}, wantUpdates: 3},
		{name: "percentage", maxPods: new(mk8sInteger(31)), budget: &workerMK8sLimit{Percent: new(mk8sInteger(50))}, wantUpdates: 3},
		{name: "omitted defaults", wantUpdates: 3},
		{name: "omitted strategy", absentStrategy: true, wantUpdates: 3},
		{name: "zero budget is raised", maxPods: new(mk8sInteger(32)), budget: &workerMK8sLimit{Count: new(mk8sInteger(0))}, wantUpdates: 3},
		{name: "unaccepted forward request", maxPods: new(mk8sInteger(32)), rejected: true, wantUpdates: 1},
		{name: "explicit default capacity", maxPods: new(mk8sInteger(0)), wantUpdates: 3},
		{name: "existing concurrent budget", maxPods: new(mk8sInteger(32)), budget: &workerMK8sLimit{Count: new(mk8sInteger(2))}, wantUpdates: 2},
		{name: "percentage concurrent budget", maxPods: new(mk8sInteger(32)), budget: &workerMK8sLimit{Percent: new(mk8sInteger(100))}, wantUpdates: 2},
		{name: "larger budget", maxPods: new(mk8sInteger(32)), budget: &workerMK8sLimit{Count: new(mk8sInteger(3))}, wantUpdates: 2},
		{name: "lost forward response", maxPods: new(mk8sInteger(32)), responseLost: 1, wantUpdates: 3},
		{name: "lost capacity restore response", maxPods: new(mk8sInteger(32)), responseLost: 2, wantUpdates: 3},
		{name: "lost budget restore response", maxPods: new(mk8sInteger(32)), responseLost: 3, wantUpdates: 3},
		{name: "failed forward operation", maxPods: new(mk8sInteger(32)), forwardFailure: true, wantUpdates: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			maxPods, budget := tc.maxPods, tc.budget
			version, updates := mk8sInteger(1), 0
			pendingOperationReads, pendingGroupReads := 0, 0
			var observedArgs [][]string
			groupJSON := func() string {
				template := map[string]any{"cloud_init_user_data": "private-user-data"}
				if maxPods != nil {
					template["max_pods"] = *maxPods
				}
				strategy := map[string]any{"max_surge": map[string]any{"count": 0}, "drain_timeout": "0s"}
				if tc.absentStrategy {
					strategy = make(map[string]any)
				}
				if budget != nil {
					strategy["max_unavailable"] = budget
				}
				effective := budget
				if effective == nil {
					effective = &workerMK8sLimit{Count: new(mk8sInteger(1))}
				}
				spec := map[string]any{"autoscaling": map[string]any{"max_node_count": 2}, "template": template}
				if len(strategy) != 0 {
					spec["strategy"] = strategy
				}
				ready := 2
				if pendingGroupReads > 0 {
					ready, pendingGroupReads = 0, pendingGroupReads-1
				} else {
					pendingOperationReads = 0
				}
				data, err := json.Marshal(map[string]any{
					"metadata": map[string]any{"id": "group-1", "resource_version": version},
					"spec":     spec,
					"status": map[string]any{"state": "RUNNING", "target_node_count": 2, "node_count": 2, "ready_node_count": ready,
						"strategy": map[string]any{"max_surge": map[string]any{"count": 0}, "drain_timeout": "0s", "max_unavailable": effective}},
				})
				require.NoError(t, err)
				return string(data)
			}
			runtime := &workerMK8sTestRuntime{}
			runtime.controller = func(string) (string, error) {
				return "NodeName=worker-0 State=IDLE\nNodeName=worker-1 State=IDLE", nil
			}
			runtime.kube = func(args ...string) (string, error) {
				var pods []corev1.Pod
				var nodes []corev1.Node
				for i := range 2 {
					host := fmt.Sprintf("host-%d", i)
					pod := readyWorkerPod(fmt.Sprintf("worker-%d", i), types.UID(fmt.Sprintf("pod-%d", i)), nil)
					pod.Namespace, pod.Spec.NodeName = framework.SoperatorNamespace, host
					pods = append(pods, pod)
					nodes = append(nodes, readyWorkerMK8sHost(host, "group-1"))
				}
				var object any = corev1.PodList{Items: pods}
				if slices.Contains(args, "nodes") {
					object = corev1.NodeList{Items: nodes}
				}
				data, err := json.Marshal(object)
				return string(data), err
			}
			runtime.local = func(args ...string) (string, error) {
				if slices.Contains(args, "compute") {
					t.Fatal("configuration restoration must not require Compute instance identities")
					return "", nil
				}
				if slices.Contains(args, "operation") {
					if tc.forwardFailure && updates == 1 {
						return `{"id":"operation-1","status":{"code":8}}`, nil
					}
					if pendingOperationReads > 0 {
						pendingOperationReads--
						return fmt.Sprintf(`{"id":"operation-%d"}`, updates), nil
					}
					return fmt.Sprintf(`{"id":"operation-%d","status":{}}`, updates), nil
				}
				if !slices.Contains(args, "update") {
					return groupJSON(), nil
				}
				observedArgs = append(observedArgs, slices.Clone(args))
				updates++
				if tc.rejected {
					return "", fmt.Errorf("request rejected before acceptance")
				}
				expected := workerMK8sPatchArgs("group-1", version)
				switch updates {
				case 1:
					changed, err := nextWorkerMK8sMaxPods(maxPods)
					require.NoError(t, err)
					expected = append(expected, "--template-max-pods", fmt.Sprint(changed))
					if budget == nil {
						budget = &workerMK8sLimit{Count: new(mk8sInteger(2))}
						expected = appendWorkerMK8sLimitArgs(expected, budget)
					} else {
						count, err := workerMK8sLimitCount(*budget, 2, false)
						require.NoError(t, err)
						if count < 2 {
							budget = &workerMK8sLimit{Count: new(mk8sInteger(2))}
							expected = appendWorkerMK8sLimitArgs(expected, budget)
						}
					}
					maxPods = new(changed)
				case 2:
					require.Zero(t, pendingGroupReads, "await forward provider convergence before restoring capacity")
					if tc.maxPods == nil {
						expected = append(expected, "--clear-mask", "spec.template.max_pods")
					} else {
						expected = append(expected, "--template-max-pods", fmt.Sprint(*tc.maxPods))
					}
					maxPods = tc.maxPods
					require.False(t, sameWorkerMK8sLimit(budget, tc.budget) && tc.wantUpdates == 3,
						"the faster budget must remain in place during capacity restoration")
				case 3:
					expected = appendWorkerMK8sLimitArgs(expected, tc.budget)
					budget = tc.budget
					require.Zero(t, pendingGroupReads, "await restored capacity convergence before reducing its rollout budget")
				default:
					t.Fatalf("unexpected extra update %d", updates)
				}
				require.Equal(t, expected, args, "patch only owned fields with the latest resource version")
				version++
				pendingOperationReads, pendingGroupReads = 1, 1
				if updates == tc.responseLost {
					return "", fmt.Errorf("response lost after acceptance")
				}
				return fmt.Sprintf("operation-%d", updates), nil
			}
			steps := &WorkerRollingUpdate{
				runtime: runtime, kubectl: framework.NewKubectlClient(runtime), slurm: framework.NewSlurmClient(runtime),
				nodeSet:     kubeobjects.NodeSet{Metadata: kubeobjects.ObjectMeta{Name: "worker", Namespace: framework.SoperatorNamespace}},
				workerNames: []string{"worker-0", "worker-1"},
				workers:     []framework.WorkerInfo{{Name: "worker-0"}, {Name: "worker-1"}},
				initialPods: map[string]workerPodSnapshot{
					"worker-0": {UID: "pod-0", NodeName: "host-0"}, "worker-1": {UID: "pod-1", NodeName: "host-1"}},
			}
			require.NoError(t, steps.selectMK8sGroup(t.Context()))
			fingerprint := steps.mk8s.specFingerprint
			err := steps.changeMK8sGroup(t.Context())
			if tc.rejected {
				require.ErrorContains(t, err, "request rejected")
			} else if tc.responseLost == 1 {
				require.ErrorContains(t, err, "response lost")
			} else {
				require.NoError(t, err)
			}
			err = steps.restoreMK8sGroup(t.Context())
			if tc.responseLost > 1 {
				require.ErrorContains(t, err, "response lost")
				require.NotNil(t, steps.mk8s.requestedMaxPods, "lost responses must retain cleanup ownership")
				err = steps.restoreMK8sGroup(t.Context())
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantUpdates, updates)
			assert.True(t, sameWorkerMK8sMaxPods(maxPods, tc.maxPods))
			assert.True(t, sameWorkerMK8sLimit(budget, tc.budget))
			group, err := decodeWorkerMK8sGroup(groupJSON())
			require.NoError(t, err)
			assert.Equal(t, fingerprint, group.fingerprint, "preserve every unowned field")
			assert.Nil(t, steps.mk8s.requestedMaxPods)
			calls := len(observedArgs)
			require.NoError(t, steps.restoreMK8sGroup(t.Context()))
			assert.Len(t, observedArgs, calls, "completed restoration must be cheap and idempotent")
		})
	}
}

func TestWorkerMK8sRejectsMalformedSpecBudget(t *testing.T) {
	for _, value := range []string{`{}`, `{"count":1,"percent":50}`, `{"percent":101}`, `{"count":-1}`} {
		input := strings.Replace(workerMK8sAutoscalingLiveShape, `"spec":{`, `"spec":{"strategy":{"max_unavailable":`+value+`},`, 1)
		_, err := decodeWorkerMK8sGroup(input)
		require.ErrorContains(t, err, "specification max_unavailable")
	}
}
