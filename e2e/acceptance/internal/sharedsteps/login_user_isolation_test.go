package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

func memoryTestEvents() map[string]int64 {
	return map[string]int64{"high": 3, "max": 0, "oom": 0, "oom_kill": 0}
}

func memoryTestSession() loginMemorySession {
	swapMax := "0"
	return loginMemorySession{
		loginMemoryEnvironment: loginMemoryEnvironment{SwapDisabled: true},
		Groups:                 []string{"regular"},
		UID:                    1000, Hostname: "login-0", Kernel: "6.8",
		Session: "/sys/fs/cgroup/container/users/user-1000",
		Memory: loginMemorySnapshot{
			Cgroup: "/sys/fs/cgroup/container/users/user-1000",
			High:   strconv.FormatInt(loginMemoryHigh, 10), Max: strconv.FormatInt(loginMemoryMax, 10),
			SwapMax: &swapMax, Events: memoryTestEvents(),
		},
	}
}

func TestLoginMemorySessions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*loginMemorySession)
		valid  bool
	}{
		{"plain SSH", func(*loginMemorySession) {}, true},
		{"Docker session", func(s *loginMemorySession) { s.Session += "/sessions" }, true},
		{"swap disabled for user", func(s *loginMemorySession) { s.Swap = true }, true},
		{"missing peak on older kernel", func(s *loginMemorySession) { s.Memory.Peak = nil }, true},
		{"swap accounting unavailable", func(s *loginMemorySession) { s.Swap = true; s.Memory.SwapMax = nil }, true},
		{"PSI unavailable", func(s *loginMemorySession) { s.Memory.Pressure = nil }, true},
		{"system user", func(s *loginMemorySession) { s.UID = 999 }, false},
		{"wrong user", func(s *loginMemorySession) { s.UID = 1001 }, false},
		{"unrestricted session", func(s *loginMemorySession) { s.Session = "/sys/fs/cgroup/container/init" }, false},
		{"wrong high", func(s *loginMemorySession) { s.Memory.High = "max" }, false},
		{"wrong max", func(s *loginMemorySession) { s.Memory.Max = "max" }, false},
		{"swap bypass", func(s *loginMemorySession) {
			s.Swap = true
			s.SwapDisabled = false
			value := "max"
			s.Memory.SwapMax = &value
		}, false},
		{"missing events", func(s *loginMemorySession) { delete(s.Memory.Events, "oom_kill") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := memoryTestSession()
			tc.change(&session)
			err := validateLoginMemorySession(session)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestLoginMemoryPrerequisites(t *testing.T) {
	for _, tc := range []struct {
		name  string
		login string
		skip  bool
	}{
		{"enabled", `{"size":1,"userIsolation":{"enabled":true},"sshd":{"resources":{"memory":"2Gi"}}}`, false},
		{"unlimited container", `{"size":1,"userIsolation":{"enabled":true}}`, false},
		{"no login", `{"size":0,"userIsolation":{"enabled":true}}`, true},
		{"disabled", `{"size":1,"userIsolation":{"enabled":false}}`, true},
		{"unset", `{"size":1}`, true},
		{"too small", `{"size":1,"userIsolation":{"enabled":true},"sshd":{"resources":{"memory":"1Gi"}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cluster kubeobjects.SlurmCluster
			require.NoError(t, json.Unmarshal([]byte(`{"spec":{"slurmNodes":{"login":`+tc.login+`}}}`), &cluster))
			reason, err := loginMemoryPrerequisite(cluster)
			require.NoError(t, err)
			assert.Equal(t, tc.skip, reason != "")
		})
	}
	var malformed kubeobjects.SlurmCluster
	malformed.Spec.SlurmNodes.Login.Size = 1
	malformed.Spec.SlurmNodes.Login.UserIsolation = json.RawMessage(`{"enabled":"yes"}`)
	_, err := loginMemoryPrerequisite(malformed)
	require.Error(t, err)
}

func TestLoginMemoryRestoration(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{`{"enabled":true,"cpuWeight":200}`, `{"memoryHigh":null,"memoryMax":null}`},
		{`{"enabled":true,"memoryHigh":"1Gi","memoryMax":"2Gi","cpuWeight":200}`, `{"memoryHigh":"1Gi","memoryMax":"2Gi"}`},
	} {
		patch, err := loginMemoryRestorePatch(json.RawMessage(tc.raw))
		require.NoError(t, err)
		data, err := json.Marshal(patch)
		require.NoError(t, err)
		assert.JSONEq(t, tc.want, string(data))
	}
	assert.True(t, loginMemoryLimitsMatch(map[string]any{"memoryHigh": "480Mi", "memoryMax": "512Mi"}))
	assert.True(t, loginMemoryLimitsMatch(map[string]any{"memoryHigh": "503316480", "memoryMax": "536870912"}))
	assert.False(t, loginMemoryLimitsMatch(map[string]any{"memoryHigh": nil, "memoryMax": nil}))
	assert.False(t, loginMemoryLimitsMatch(map[string]any{"memoryHigh": "1Gi", "memoryMax": "2Gi"}))
	_, err := loginMemoryRestorePatch(json.RawMessage(`{`))
	require.Error(t, err)
}

func memoryTestPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "login-0", UID: types.UID("original"), Labels: map[string]string{"controller-revision-hash": "new"}},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "sshd", RestartCount: 1}},
		},
	}
}

func memoryTestStatefulSet() kubeobjects.LoginStatefulSet {
	var sts kubeobjects.LoginStatefulSet
	replicas := int32(1)
	sts.Metadata.Generation = 2
	sts.Spec.Replicas = &replicas
	sts.Status.ObservedGeneration = 2
	sts.Status.UpdateRevision = "new"
	sts.Status.CurrentRevision = "new"
	sts.Status.ReadyReplicas = 1
	sts.Status.UpdatedReadyReplicas = 1
	return sts
}

func TestLoginMemoryRollout(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*kubeobjects.LoginStatefulSet, *corev1.Pod)
	}{
		{"old revision", func(s *kubeobjects.LoginStatefulSet, _ *corev1.Pod) { s.Status.UpdateRevision = "old" }},
		{"unobserved generation", func(s *kubeobjects.LoginStatefulSet, _ *corev1.Pod) { s.Status.ObservedGeneration = 1 }},
		{"partial rollout", func(s *kubeobjects.LoginStatefulSet, _ *corev1.Pod) { s.Status.UpdatedReadyReplicas = 0 }},
		{"unready pod", func(_ *kubeobjects.LoginStatefulSet, p *corev1.Pod) { p.Status.Conditions = nil }},
		{"pod old revision", func(_ *kubeobjects.LoginStatefulSet, p *corev1.Pod) { p.Labels["controller-revision-hash"] = "old" }},
		{"terminating pod", func(_ *kubeobjects.LoginStatefulSet, p *corev1.Pod) { now := metav1.Now(); p.DeletionTimestamp = &now }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sts, pod := memoryTestStatefulSet(), memoryTestPod()
			require.NoError(t, loginMemoryRolloutReady(sts, []corev1.Pod{pod}, "old", true))
			tc.change(&sts, &pod)
			require.Error(t, loginMemoryRolloutReady(sts, []corev1.Pod{pod}, "old", true))
		})
	}
	require.Error(t, loginMemoryRolloutReady(memoryTestStatefulSet(), nil, "old", true))
	require.NoError(t, loginMemoryRolloutReady(memoryTestStatefulSet(), []corev1.Pod{memoryTestPod()}, "new", false))
}

func TestLoginMemoryRolloutWithStaleCurrentRevision(t *testing.T) {
	for _, strategy := range []string{"", "RollingUpdate", "OnDelete"} {
		t.Run(strategy, func(t *testing.T) {
			sts := memoryTestStatefulSet()
			sts.Spec.UpdateStrategy.Type = strategy
			sts.Status.CurrentRevision = "old"
			replicas := int32(2)
			sts.Spec.Replicas = &replicas
			sts.Status.ReadyReplicas, sts.Status.UpdatedReadyReplicas = replicas, replicas
			pods := []corev1.Pod{memoryTestPod(), memoryTestPod()}
			pods[1].Name = "login-1"
			err := loginMemoryRolloutReady(sts, pods, "old", true)
			if strategy != "OnDelete" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			pods[1].Labels["controller-revision-hash"] = "old"
			require.Error(t, loginMemoryRolloutReady(sts, pods, "old", true), "every pod must reach the target revision")
		})
	}
}

func memoryTestWorkload(hard bool) loginMemoryWorkload {
	session := memoryTestSession()
	result := loginMemoryWorkload{Before: session.Memory, After: memoryTestSession().Memory}
	result.After.Events["high"]++
	result.Allocation.Held = session.Memory
	result.Allocation.Held.Current = loginMemoryHigh + 1024*1024
	result.Allocation.SampledPeak = result.Allocation.Held.Current
	result.Allocation.HeldSeconds = loginMemoryHold.Seconds()
	result.Allocation.Samples = []loginMemorySample{
		{Current: result.Allocation.Held.Current, HighEvents: result.After.Events["high"]},
		{Current: result.Allocation.Held.Current, HighEvents: result.After.Events["high"], ElapsedSeconds: loginMemoryHold.Seconds()},
	}
	if hard {
		result.ReturnCode = -9
		result.After.Events["oom"]++
		result.After.Events["oom_kill"]++
		peak := loginMemoryMax
		result.After.Peak = &peak
	}
	return result
}

func TestLoginMemoryWorkloads(t *testing.T) {
	for _, hard := range []bool{false, true} {
		result := memoryTestWorkload(hard)
		require.NoError(t, validateLoginMemoryWorkload(result, hard))
		result.TimedOut = true
		require.Error(t, validateLoginMemoryWorkload(result, hard), "a watchdog SIGKILL must not count as an OOM kill")
		result = memoryTestWorkload(hard)
		result.Allocation.LimitBypassed = true
		require.Error(t, validateLoginMemoryWorkload(result, hard))
		result = memoryTestWorkload(hard)
		result.After.Events = result.Before.Events
		require.Error(t, validateLoginMemoryWorkload(result, hard))
	}
	result := memoryTestWorkload(false)
	result.After.Events["oom_kill"]++
	require.Error(t, validateLoginMemoryWorkload(result, false))
	result = memoryTestWorkload(true)
	result.ReturnCode = 137
	require.Error(t, validateLoginMemoryWorkload(result, true))
	result = memoryTestWorkload(true)
	*result.After.Peak = loginMemoryMax + 1
	require.NoError(t, validateLoginMemoryWorkload(result, true), "the kernel permits transient memory.max overshoot")
	result.After.Peak = nil
	require.NoError(t, validateLoginMemoryWorkload(result, true))
}

func TestLoginMemoryNeighborAndContainerCounters(t *testing.T) {
	before, after := memoryTestEvents(), memoryTestEvents()
	require.NoError(t, loginMemoryEventsUnchanged(before, after, "high", "oom", "oom_kill"))
	after["oom_kill"]++
	require.Error(t, loginMemoryEventsUnchanged(before, after, "oom_kill"))
	assert.Equal(t, int64(1), loginMemoryEventDeltas(before, after)["oom_kill"])
	delete(after, "oom")
	require.Error(t, loginMemoryEventsUnchanged(before, after, "oom"))
}

func TestLoginMemoryServicesDetectsKillsOutsideTarget(t *testing.T) {
	baseline := func() loginMemoryServices {
		return loginMemoryServices{ContainerLocal: memoryTestEvents(), Container: memoryTestEvents(), Target: memoryTestEvents(), Init: memoryTestEvents()}
	}
	for _, change := range []func(*loginMemoryServices){
		func(s *loginMemoryServices) { s.Container["oom_kill"]++ },
		func(s *loginMemoryServices) { s.ContainerLocal["oom"]++ },
		func(s *loginMemoryServices) { s.Init["oom_kill"]++ },
		func(s *loginMemoryServices) { delete(s.Target, "oom_kill") },
	} {
		before, after := baseline(), baseline()
		after.Container["oom_kill"], after.Target["oom_kill"] = 1, 1
		require.NoError(t, validateLoginMemoryServices(before, after))
		change(&after)
		require.Error(t, validateLoginMemoryServices(before, after))
	}
}

func TestLoginMemoryHighUsesObservationWindow(t *testing.T) {
	result := memoryTestWorkload(false)
	result.Allocation.Held.Current = loginMemoryHigh - 1024
	result.Allocation.Samples[1].Current = result.Allocation.Held.Current
	require.NoError(t, validateLoginMemoryWorkload(result, false), "later reclaim must not erase an observed threshold crossing")
	result.Allocation.Samples[0].Current = loginMemoryHigh - 1024
	require.Error(t, validateLoginMemoryWorkload(result, false), "require a sample proving the threshold crossing")
	result = memoryTestWorkload(false)
	result.Allocation.HeldSeconds = loginMemoryHold.Seconds() - 0.01
	require.Error(t, validateLoginMemoryWorkload(result, false))
}

func TestLoginMemoryPythonInheritedSwapLimit(t *testing.T) {
	runLoginMemoryPython(t, `import tempfile
with tempfile.TemporaryDirectory() as directory:
    root = pathlib.Path(directory)
    parent = root / "container"
    group = parent / "users" / "user-1000"
    group.mkdir(parents=True)
    swaps = root / "swaps"
    swaps.write_text("Filename Type Size Used Priority\n/swap file 1024 0 -2\n")
    (group / "memory.swap.max").write_text("max")
    unrestricted = scope["environment"](group, root, swaps)
    assert unrestricted["swap"] and not unrestricted["swap_disabled"], unrestricted
    (parent / "memory.swap.max").write_text("0")
    inherited = scope["environment"](group, root, swaps)
    assert inherited["swap"] and inherited["swap_disabled"], inherited
    assert inherited["swap_limits"][str(group)] == "max" and inherited["swap_limits"][str(parent)] == "0"
    (group / "memory.swap.max").unlink()
    assert scope["environment"](group, root, swaps)["swap_disabled"]
    swaps.write_text("Filename Type Size Used Priority\n")
    assert not scope["environment"](group, root, swaps)["swap"]
`)
}

func TestLoginMemoryPythonSamplesHighDuringHold(t *testing.T) {
	runLoginMemoryPython(t, `import tempfile
config = scope["parse_arguments"](["--memory-high", "2097152", "--memory-max", "4194304", "--held-bytes", "16", "--hold-seconds", "0.3", "--workload-timeout", "1", "high"])
captured = {}
clock = [0.0]
event_reads = [0]
with tempfile.TemporaryDirectory() as directory:
    group = pathlib.Path(directory)
    for filename, value in {"memory.high": "2097152", "memory.max": "4194304", "memory.current": "2621440"}.items():
        (group / filename).write_text(value)
    scope["user_cgroup"] = lambda: group
    def events(group):
        event_reads[0] += 1
        return {"high": 0 if event_reads[0] == 1 else 1}
    def sleep(duration):
        assert duration > 0
        clock[0] += duration
        (group / "memory.current").write_text("1048576")
    scope["counters"] = events
    scope["snapshot"] = lambda group: {"current": 1048576}
    scope["time"].monotonic = lambda: clock[0]
    scope["time"].sleep = sleep
    scope["emit"] = lambda result: captured.update(result)
    scope["allocate"]("high", config)
assert captured["held_seconds"] >= 0.3 and len(captured["samples"]) >= 2, captured
assert captured["samples"][0]["current"] > config.memory_high
assert captured["held"]["current"] < config.memory_high and captured["sampled_peak"] > config.memory_high
`)
}

func TestLoginMemoryPythonSamplesAtHoldDeadline(t *testing.T) {
	runLoginMemoryPython(t, `import tempfile
from types import SimpleNamespace
config = SimpleNamespace(memory_high=2097152, memory_max=4194304, hold_seconds=5)
captured = {}
clock = iter([0, 0, 4.999999, 5.000001, 5.000002, 5.000003])
event_reads = [0]
with tempfile.TemporaryDirectory() as directory:
    group = pathlib.Path(directory)
    for filename, value in {"memory.high": "2097152", "memory.max": "4194304", "memory.current": "2621440"}.items():
        (group / filename).write_text(value)
    scope["user_cgroup"] = lambda: group
    def events(group):
        event_reads[0] += 1
        return {"high": 0 if event_reads[0] == 1 else 1}
    scope["counters"] = events
    scope["snapshot"] = lambda group: {"current": 2621440}
    scope["time"].monotonic = lambda: next(clock)
    scope["time"].sleep = lambda duration: None
    scope["emit"] = lambda result: captured.update(result)
    scope["allocate"]("high", config)
assert captured["held_seconds"] >= 5, captured
assert captured["samples"][-1]["elapsed_seconds"] >= 5, captured
assert captured["samples"][-2]["elapsed_seconds"] < 5, captured
`)
}

type memoryNeighborRuntime struct {
	framework.Runtime
	responses []string
	calls     int
}

func (r *memoryNeighborRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(context.Context, string) (string, error) {
		index := min(r.calls, len(r.responses)-1)
		r.calls++
		return r.responses[index], nil
	})
}

func (r *memoryNeighborRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	for range 5 {
		if err := ctx.Err(); err != nil {
			return err
		}
		done, err := condition(ctx)
		if err == nil && done {
			return nil
		}
	}
	return fmt.Errorf("neighbor readiness timed out")
}

func TestLoginMemoryNeighborDefinitiveFailuresAreNotRetried(t *testing.T) {
	for _, change := range []func(*loginMemoryNeighbor){
		func(s *loginMemoryNeighbor) { s.Process.UID = 2000 },
		func(s *loginMemoryNeighbor) { s.Memory.Cgroup = "/wrong" },
		func(s *loginMemoryNeighbor) { s.Memory.Current-- },
		func(s *loginMemoryNeighbor) { s.Memory.Events["oom_kill"]++ },
		func(s *loginMemoryNeighbor) { s.Error = "AssertionError: checksum mismatch" },
		func(s *loginMemoryNeighbor) { s.Ready = nil },
	} {
		ready := true
		result := loginMemoryNeighbor{Ready: &ready, Memory: memoryTestSession().Memory}
		result.Process.UID = 1000
		result.Memory.Current = loginMemoryHeld
		change(&result)
		data, err := json.Marshal(result)
		require.NoError(t, err)
		runtime := &memoryNeighborRuntime{responses: []string{"LOGIN_MEMORY=" + string(data)}}
		s := NewLoginUserIsolation(nil, runtime, nil)
		s.report = make(map[string]any)
		s.sessions[1] = memoryTestSession()
		s.neighbor = memoryTestSession().Memory
		require.Error(t, s.checkNeighborMemory(t.Context()))
		assert.Equal(t, 1, runtime.calls)
	}
}

func TestLoginMemoryNeighborWaitsForChecksumResponse(t *testing.T) {
	ready := true
	result := loginMemoryNeighbor{Ready: &ready, Memory: memoryTestSession().Memory}
	result.Process.UID, result.Memory.Current = 1000, loginMemoryHeld
	data, err := json.Marshal(result)
	require.NoError(t, err)
	runtime := &memoryNeighborRuntime{responses: []string{`LOGIN_MEMORY={"ready":false}`, "LOGIN_MEMORY=" + string(data)}}
	s := NewLoginUserIsolation(nil, runtime, nil)
	s.report = make(map[string]any)
	s.sessions[1] = memoryTestSession()
	s.neighbor = memoryTestSession().Memory
	require.NoError(t, s.checkNeighborMemory(t.Context()))
	assert.Equal(t, 2, runtime.calls)
}

func TestLoginMemoryPodHealth(t *testing.T) {
	before := snapshotLoginMemoryPods([]corev1.Pod{memoryTestPod()})
	for _, mutate := range []func(*corev1.Pod){
		func(p *corev1.Pod) { p.UID = "replacement" },
		func(p *corev1.Pod) { p.Status.ContainerStatuses[0].RestartCount++ },
		func(p *corev1.Pod) {
			p.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{Reason: "OOMKilled"}
		},
		func(p *corev1.Pod) { p.Status.Conditions = nil },
	} {
		pod := memoryTestPod()
		require.NoError(t, loginMemoryPodsUnchanged(before, snapshotLoginMemoryPods([]corev1.Pod{pod})))
		mutate(&pod)
		require.Error(t, loginMemoryPodsUnchanged(before, snapshotLoginMemoryPods([]corev1.Pod{pod})))
	}
}

func TestLoginMemoryPreviousOOMDoesNotFailScenario(t *testing.T) {
	pod := memoryTestPod()
	pod.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{
		Reason: "OOMKilled", ExitCode: 137, FinishedAt: metav1.NewTime(time.Unix(100, 0)),
	}
	before := snapshotLoginMemoryPods([]corev1.Pod{pod})
	require.NoError(t, loginMemoryPodsUnchanged(before, snapshotLoginMemoryPods([]corev1.Pod{pod})))
	pod.Status.ContainerStatuses[0].LastTerminationState.Terminated.FinishedAt = metav1.NewTime(time.Unix(200, 0))
	require.Error(t, loginMemoryPodsUnchanged(before, snapshotLoginMemoryPods([]corev1.Pod{pod})))
	assert.Equal(t, int64(100), before[pod.Name].Containers["sshd"].LastTermination.FinishedAt.Unix(), "snapshot must retain the original history")
}

func TestLoginMemoryPythonOptionalMetrics(t *testing.T) {
	runLoginMemoryPython(t, `import tempfile
from unittest.mock import patch
with tempfile.TemporaryDirectory() as directory:
    group = pathlib.Path(directory)
    for filename, value in {"memory.current": "16", "memory.high": "64", "memory.max": "128", "memory.events": "high 0\nmax 0\noom 0\noom_kill 0\n"}.items():
        (group / filename).write_text(value)
    missing = scope["snapshot"](group)
    assert missing["swap_max"] is None and missing["pressure"] is None and missing["peak"] is None, missing
    for filename, value in {"memory.swap.max": "0", "memory.pressure": "some avg10=0.00", "memory.peak": "32"}.items():
        (group / filename).write_text(value)
    present = scope["snapshot"](group)
    assert present["swap_max"] == "0" and present["peak"] == 32 and present["pressure"] == "some avg10=0.00", present
    original = pathlib.Path.read_text
    def read_text(path, *args, **kwargs):
        if path.name == "memory.pressure":
            raise PermissionError("denied PSI")
        return original(path, *args, **kwargs)
    with patch.object(pathlib.Path, "read_text", read_text):
        try:
            scope["snapshot"](group)
        except PermissionError:
            pass
        else:
            raise AssertionError("ignored an unreadable metric")
    (group / "memory.events").unlink()
    try:
        scope["snapshot"](group)
    except FileNotFoundError:
        pass
    else:
        raise AssertionError("ignored missing mandatory OOM counters")
`)
}

func TestLoginMemoryPythonWatchdogAndConfiguration(t *testing.T) {
	runLoginMemoryPython(t, `import tempfile
config = scope["parse_arguments"](["--memory-high", "64", "--memory-max", "128", "--held-bytes", "16", "--hold-seconds", "0.3", "--workload-timeout", "0.1", "hard"])
captured = {}
class Child:
    returncode = None
    def poll(self):
        return self.returncode
    def kill(self):
        self.returncode = -9
        captured["killed"] = True
    def communicate(self):
        return "", ""
def start(arguments, **kwargs):
    captured["arguments"] = arguments
    return Child()
with tempfile.TemporaryDirectory() as directory:
    group = pathlib.Path(directory)
    (group / "memory.current").write_text("100")
    scope["user_cgroup"] = lambda: group
    scope["snapshot"] = lambda group: {"high": "64", "max": "128"}
    scope["subprocess"].Popen = start
    clock = iter([10.0, 10.2])
    scope["time"].monotonic = lambda: next(clock)
    scope["emit"] = lambda result: captured.update(result)
    scope["workload"]("hard", config)
assert captured["killed"] and captured["timed_out"] and captured["returncode"] == -9, captured
child_config = scope["parse_arguments"](captured["arguments"][2:])
assert child_config.memory_high == 64 and child_config.memory_max == 128 and child_config.held_bytes == 16
assert child_config.workload_timeout == 0.1 and child_config.action == "allocate" and child_config.arguments == ["hard"]
`)
}

func TestLoginMemoryPythonDrainsAllocatorOutput(t *testing.T) {
	runLoginMemoryPython(t, `import tempfile
config = scope["parse_arguments"](["--memory-high", "64", "--memory-max", "128", "--held-bytes", "16", "--hold-seconds", "0.3", "--workload-timeout", "5", "high"])
captured = {}
original_popen = scope["subprocess"].Popen
def start(arguments, **kwargs):
    return original_popen([sys.executable, "-c", "import json; print('LOGIN_MEMORY=' + json.dumps({'buffer': 'x' * 131072}))"], **kwargs)
with tempfile.TemporaryDirectory() as directory:
    group = pathlib.Path(directory)
    (group / "memory.current").write_text("100")
    scope["user_cgroup"] = lambda: group
    scope["snapshot"] = lambda group: {"high": "64", "max": "128"}
    scope["subprocess"].Popen = start
    scope["emit"] = lambda result: captured.update(result)
    scope["workload"]("high", config)
assert not captured["timed_out"] and captured["returncode"] == 0
assert len(captured["allocation"]["buffer"]) == 131072
`)
}

func TestLoginMemoryReportRetainsBaselinesAndWritesOnCleanup(t *testing.T) {
	s := &LoginUserIsolation{report: make(map[string]any), artifactDir: t.TempDir()}
	filename := filepath.Join(s.artifactDir, "login-memory-isolation.json")
	for _, prefix := range []string{"services", "neighbor"} {
		require.NoError(t, s.decodeProbe(prefix+"_before", `LOGIN_MEMORY={"value":1}`, new(map[string]int)))
		require.NoError(t, s.decodeProbe(prefix+"_after", `LOGIN_MEMORY={"value":2}`, new(map[string]int)))
	}
	pod := memoryTestPod()
	pod.Spec.Containers = []corev1.Container{{Name: "sshd", Image: "large-spec-must-not-be-serialized"}}
	s.record("pods_before", snapshotLoginMemoryPods([]corev1.Pod{pod}))
	_, err := os.Stat(filename)
	require.ErrorIs(t, err, os.ErrNotExist, "recording probes must not rewrite the report")
	s.writeReport()
	initial, err := os.ReadFile(filename)
	require.NoError(t, err)
	assert.NotContains(t, string(initial), "large-spec-must-not-be-serialized")
	var report map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(initial, &report))
	assert.JSONEq(t, `{"value":1}`, string(report["services_before"]))
	assert.JSONEq(t, `{"value":2}`, string(report["services_after"]))
	assert.JSONEq(t, `{"value":1}`, string(report["neighbor_before"]))
	assert.JSONEq(t, `{"value":2}`, string(report["neighbor_after"]))
	s.record("final_result", "retained")
	unchanged, err := os.ReadFile(filename)
	require.NoError(t, err)
	assert.Equal(t, initial, unchanged)
	s.CleanupAndReset(t.Context())
	final, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(final, &report))
	assert.JSONEq(t, `"retained"`, string(report["final_result"]))
	assert.Nil(t, s.report)
}

func TestLoginMemoryProbeCommandParameters(t *testing.T) {
	s := &LoginUserIsolation{workdir: "/tmp/login memory"}
	for _, tc := range []struct {
		action  string
		timeout time.Duration
	}{
		{"high", loginMemoryHighTimeout},
		{"hard", loginMemoryHardTimeout},
	} {
		command := "python3() { printf '%s\\n' \"$@\"; }\n" + s.probeCommand(tc.action, "argument'with quote")
		output, err := exec.CommandContext(t.Context(), "bash", "-c", command).CombinedOutput()
		require.NoError(t, err, string(output))
		assert.Equal(t, []string{
			"/tmp/login memory/probe.py", "--memory-high", strconv.FormatInt(loginMemoryHigh, 10),
			"--memory-max", strconv.FormatInt(loginMemoryMax, 10), "--held-bytes", strconv.FormatInt(loginMemoryHeld, 10), "--hold-seconds", strconv.FormatFloat(loginMemoryHold.Seconds(), 'f', 0, 64),
			"--workload-timeout", strconv.FormatFloat(tc.timeout.Seconds(), 'f', 0, 64), tc.action, "argument'with quote",
		}, strings.Split(strings.TrimSuffix(string(output), "\n"), "\n"))
	}
	assert.Greater(t, loginMemoryCommand, loginMemoryHardTimeout)
}

func TestLoginMemoryPythonCgroupDiscovery(t *testing.T) {
	script := `scope["os"].getuid = lambda: 1000
for suffix in ("", "/sessions"):
    scope["process_cgroup"] = lambda: pathlib.Path("/sys/fs/cgroup/container/users/user-1000" + suffix)
    assert str(scope["user_cgroup"]()) == "/sys/fs/cgroup/container/users/user-1000"
scope["process_cgroup"] = lambda: pathlib.Path("/sys/fs/cgroup/container/init")
try:
    scope["user_cgroup"]()
except AssertionError:
    pass
else:
    raise AssertionError("accepted unrestricted SSH session")
scope["user_cgroup"] = lambda: pathlib.Path("/unused")
scope["snapshot"] = lambda group: {"high": "max", "max": "max"}
scope["subprocess"].Popen = lambda *args, **kwargs: sys.exit("started unsafe workload")
try:
    scope["workload"]("hard", scope["parse_arguments"](["--memory-high", "64", "--memory-max", "128", "--held-bytes", "16", "--hold-seconds", "0.3", "--workload-timeout", "0.1", "hard"]))
except AssertionError:
    pass
else:
    raise AssertionError("accepted unsafe workload limits")
`
	runLoginMemoryPython(t, script)
}

func runLoginMemoryPython(t *testing.T, script string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable for the remote probe unit test")
	}
	setup := `import pathlib, sys
scope = {"__name__": "unit_test", "__file__": "probe.py"}
exec(compile(sys.stdin.read(), "probe.py", "exec"), scope)
`
	command := exec.CommandContext(t.Context(), python, "-c", setup+script)
	command.Stdin = strings.NewReader(loginMemoryProbe)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}

type loginMemoryCleanupRuntime struct {
	framework.Runtime
	commands    []string
	config      string
	statefulSet *kubeobjects.LoginStatefulSet
}

func (r *loginMemoryCleanupRuntime) Logf(string, ...any) {}

func (r *loginMemoryCleanupRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		r.commands = append(r.commands, command)
		return "", nil
	})
}

func (r *loginMemoryCleanupRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		r.commands = append(r.commands, strings.Join(args, " "))
		switch args[0] {
		case "patch":
			return "patched", nil
		case "exec":
			return r.config, nil
		case "get":
			var value any
			if args[1] == kruiseStatefulSetResource {
				sts := memoryTestStatefulSet()
				if r.statefulSet != nil {
					sts = *r.statefulSet
				}
				value = kubeobjects.LoginStatefulSetList{Items: []kubeobjects.LoginStatefulSet{sts}}
			} else {
				value = corev1.PodList{Items: []corev1.Pod{memoryTestPod()}}
			}
			data, err := json.Marshal(value)
			return string(data), err
		default:
			return "", fmt.Errorf("unexpected command: %v", args)
		}
	})
}

func (r *loginMemoryCleanupRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	done, err := condition(ctx)
	if err != nil {
		return err
	}
	if !done {
		return fmt.Errorf("condition is pending")
	}
	return nil
}

func TestLoginMemoryCleanupAfterPartialExecution(t *testing.T) {
	for _, config := range []string{"original", "test limits still mounted"} {
		t.Run(config, func(t *testing.T) {
			runtime := &loginMemoryCleanupRuntime{config: config}
			s := NewLoginUserIsolation(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, framework.NewKubectlClient(runtime))
			s.workdir = "/tmp/login-memory-test"
			s.originalLimits = map[string]any{"memoryHigh": nil, "memoryMax": nil}
			s.originalConfig = "original"
			report := make(map[string]any)
			s.report = report
			s.CleanupAndReset(t.Context())
			require.GreaterOrEqual(t, len(runtime.commands), 7)
			assert.Contains(t, runtime.commands[0], "pkill -u 'soperatormema'")
			assert.Contains(t, runtime.commands[1], "pkill -u 'soperatormemb'")
			assert.Contains(t, runtime.commands[2], "rm -rf -- '/tmp/login-memory-test'")
			assert.Contains(t, runtime.commands[3], `{"spec":{"slurmNodes":{"login":{"userIsolation":{"memoryHigh":null,"memoryMax":null}}}}}`)
			if config == "original" {
				assert.Equal(t, "restored", report["cleanup"])
			} else {
				assert.Contains(t, report["cleanup"], "wait for restored memory configuration")
			}
			assert.Nil(t, s.originalLimits)
			assert.Empty(t, s.workdir)
			assert.Nil(t, s.report)
			count := len(runtime.commands)
			s.CleanupAndReset(t.Context())
			assert.Len(t, runtime.commands, count, "cleanup must be idle after reset")
		})
	}
}

func TestLoginMemoryRolloutChecksMountedConfigAfterReadiness(t *testing.T) {
	for _, ready := range []bool{false, true} {
		sts := memoryTestStatefulSet()
		if !ready {
			sts.Status.UpdatedReadyReplicas = 0
		}
		runtime := &loginMemoryCleanupRuntime{statefulSet: &sts}
		s := NewLoginUserIsolation(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, framework.NewKubectlClient(runtime))
		checked := 0
		problem := errors.New("mounted config differs")
		_, err := s.waitMemoryRollout(t.Context(), "old", true, func(_ context.Context, pod corev1.Pod) error {
			checked++
			assert.Equal(t, "login-0", pod.Name)
			return problem
		})
		if ready {
			require.ErrorIs(t, err, problem)
			assert.Equal(t, 1, checked)
		} else {
			require.Error(t, err)
			assert.Zero(t, checked)
		}
	}
}

func TestLoginMemoryProbeParsing(t *testing.T) {
	s := &LoginUserIsolation{report: make(map[string]any)}
	var session loginMemorySession
	err := s.decodeProbe("inspect", `SSH banner
LOGIN_MEMORY={"uid":1000,"groups":["regular"],"hostname":"login-0","kernel":"6.8","session":"/sys/fs/cgroup/users/user-1000/sessions","memory":{"cgroup":"/sys/fs/cgroup/users/user-1000","high":"503316480","max":"536870912","events":{"high":0,"max":0,"oom":0,"oom_kill":0},"peak":null}}
`, &session)
	require.NoError(t, err)
	require.NoError(t, validateLoginMemorySession(session))
	assert.Nil(t, session.Memory.Peak)
	assert.Contains(t, s.report, "inspect")
	require.Error(t, s.decodeProbe("inspect", "SSH connection closed", &session))
	require.Error(t, s.decodeProbe("inspect", "LOGIN_MEMORY={", &session))
}

func TestLoginMemoryHolderPreparationAndDiagnostics(t *testing.T) {
	for _, phase := range []string{"prepare", "launch", "ready"} {
		t.Run(phase, func(t *testing.T) {
			problem := errors.New("holder " + phase + " error")
			var commands []string
			runtime := &sshTestUserRuntime{
				jail: framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
					commands = append(commands, command)
					switch {
					case strings.HasPrefix(command, "install "):
						if phase == "prepare" {
							return "", problem
						}
						return "", nil
					case strings.HasPrefix(command, "su "):
						if phase == "launch" {
							return "", problem
						}
						return "", nil
					case strings.HasPrefix(command, "test -s "):
						return "", problem
					case strings.HasPrefix(command, "ls -ld "):
						return "holder traceback from hold.log", nil
					default:
						return "", fmt.Errorf("unexpected command: %s", command)
					}
				}),
			}
			s := NewLoginUserIsolation(nil, runtime, nil)
			s.workdir = "/tmp/login-memory-test"
			s.report = make(map[string]any)
			err := s.holdNeighborMemory(t.Context())
			require.ErrorIs(t, err, problem)
			require.NotEmpty(t, commands)
			assert.Equal(t, "install -d -m 0700 -o 'soperatormemb' -- '/tmp/login-memory-test/soperatormemb'", commands[0])
			if phase == "prepare" {
				assert.Len(t, commands, 1, "directory creation must finish before SSH starts")
				assert.Empty(t, runtime.description, "directory errors must fail before polling")
				return
			}
			assert.Contains(t, commands[1], "nohup setsid")
			assert.NotContains(t, commands[1], "install -d", "background only the workload, not its directory preparation")
			if phase == "launch" {
				assert.Len(t, commands, 2)
				assert.Empty(t, runtime.description, "SSH launch errors must fail before polling")
				return
			}
			assert.Len(t, commands, 4)
			assert.Contains(t, commands[3], "hold.log")
			assert.ErrorContains(t, err, "holder traceback from hold.log")
			assert.Equal(t, "holder traceback from hold.log", s.report["neighbor_holder_diagnostics"])
		})
	}
}
