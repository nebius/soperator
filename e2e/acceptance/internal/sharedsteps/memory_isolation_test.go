package sharedsteps

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func TestMemoryReservationHookHandlesFocusedScenarioLocations(t *testing.T) {
	for _, location := range []string{"features/memory_isolation.feature", "features/memory_isolation.feature:2"} {
		t.Run(location, func(t *testing.T) {
			family := NewMemoryIsolation(nil, nil, nil, nil, nil)
			suite := godog.TestSuite{
				ScenarioInitializer: func(sc *godog.ScenarioContext) {
					registerScenarioScoped(sc, family)
					sc.Step(`^the scenario owns a reservation scope$`, func(ctx context.Context) error {
						_, err := framework.WorkerReservationName(ctx)
						if err == nil {
							return errors.New("expected an unacquired reservation scope")
						}
						return nil
					})
				},
				Options: &godog.Options{FS: fstest.MapFS{"features/memory_isolation.feature": {Data: []byte("Feature: memory ownership\n Scenario: scope\n  Then the scenario owns a reservation scope\n")}},
					Paths: []string{location}, Format: "progress", Output: io.Discard, Strict: true, DefaultContext: t.Context()},
			}
			assert.Zero(t, suite.Run())
			assert.False(t, family.ownsReservation, "cleanup must finalize the scenario-owned scope")
		})
	}
}

func memoryTestSnapshot() memorySnapshot {
	return memorySnapshot{Cgroup: "/sys/fs/cgroup/slurm/job_42", Maximum: strconv.FormatInt(memoryJobLimit, 10),
		Current: memoryJobLimit / 3, Events: map[string]int64{"max": 0, "oom": 0, "oom_kill": 0}}
}

func TestMemoryLimitRejectsMissingEnforcementAndUsableSwap(t *testing.T) {
	for _, change := range []func(*memorySnapshot){
		func(s *memorySnapshot) { s.Maximum = "max" },
		func(s *memorySnapshot) { s.Maximum = "536870912" },
		func(s *memorySnapshot) { s.Cgroup = "" },
		func(s *memorySnapshot) { s.Current = -1 },
		func(s *memorySnapshot) { delete(s.Events, "oom_kill") },
		func(s *memorySnapshot) { s.Events["oom"] = -1 },
		func(s *memorySnapshot) { s.Swap = true },
	} {
		state := memoryTestSnapshot()
		require.NoError(t, validateMemoryLimit(state))
		change(&state)
		require.Error(t, validateMemoryLimit(state))
	}
	state := memoryTestSnapshot()
	state.Swap = true
	state.SwapLimits = map[string]string{"job": "max", "ancestor": "0"}
	require.NoError(t, validateMemoryLimit(state), "an inherited swap prohibition is sufficient")
}

func TestMemoryBelowLimitRequiresHeldMemoryInSameProcess(t *testing.T) {
	ready := memoryWorkload{PID: 123, StartTicks: "456"}
	workload := ready
	workload.Allocated, workload.HeldSeconds = memoryJobLimit/4, 3
	require.NoError(t, validateMemoryBelow(workload, ready, memoryTestSnapshot()))
	for _, change := range []func(*memoryWorkload){
		func(w *memoryWorkload) { w.PID++ },
		func(w *memoryWorkload) { w.StartTicks = "reused PID" },
		func(w *memoryWorkload) { w.HeldSeconds = 0 },
		func(w *memoryWorkload) { w.Allocated = 0 },
	} {
		invalid := workload
		change(&invalid)
		require.Error(t, validateMemoryBelow(invalid, ready, memoryTestSnapshot()))
	}
	state := memoryTestSnapshot()
	state.Events["oom_kill"] = 1
	require.Error(t, validateMemoryBelow(workload, ready, state))
	state = memoryTestSnapshot()
	state.Current = workload.Allocated - 1
	require.Error(t, validateMemoryBelow(workload, ready, state))
}

func memoryTestOOM() memoryObservation {
	before, after := memoryTestSnapshot(), memoryTestSnapshot()
	before.Events["oom"], before.Events["oom_kill"] = 2, 2
	after.Events["oom"], after.Events["oom_kill"] = 3, 3
	return memoryObservation{Before: before, After: after, Finished: true, SampledPeak: memoryJobLimit + 4096}
}

func TestMemoryOOMRejectsTimeoutsAndUnrelatedKills(t *testing.T) {
	for _, change := range []func(*memoryObservation){
		func(o *memoryObservation) { o.TimedOut = true },
		func(o *memoryObservation) { o.Finished = false },
		func(o *memoryObservation) { o.Error = "observer died" },
		func(o *memoryObservation) { o.After.Cgroup += "0" },
		func(o *memoryObservation) { o.After.Events["oom_kill"] = o.Before.Events["oom_kill"] },
		func(o *memoryObservation) { o.After.Events["oom"] = o.Before.Events["oom"] },
	} {
		observation := memoryTestOOM()
		require.NoError(t, validateMemoryOOM(observation, observation.Before.Cgroup), "transient kernel memory.max overshoot is allowed")
		change(&observation)
		require.Error(t, validateMemoryOOM(observation, observation.Before.Cgroup))
	}
}

func TestMemoryNeighborRequiresFreshChecksumFromOriginalProcess(t *testing.T) {
	ready := memoryWorkload{PID: 123, StartTicks: "456", Checksum: "original memory", Allocated: memoryNeighborHeld}
	verified := ready
	verified.Challenge = "new challenge"
	require.NoError(t, validateMemoryNeighbor(ready, verified, verified.Challenge, memoryTestSnapshot(), memoryTestSnapshot()))
	for _, change := range []func(*memoryWorkload){
		func(w *memoryWorkload) { w.PID++ },
		func(w *memoryWorkload) { w.StartTicks = "restarted" },
		func(w *memoryWorkload) { w.Checksum = "corrupted" },
		func(w *memoryWorkload) { w.Challenge = "old challenge" },
		func(w *memoryWorkload) { w.Allocated = 0 },
	} {
		invalid := verified
		change(&invalid)
		require.Error(t, validateMemoryNeighbor(ready, invalid, verified.Challenge, memoryTestSnapshot(), memoryTestSnapshot()))
	}
	state := memoryTestSnapshot()
	state.Events["oom_kill"]++
	require.Error(t, validateMemoryNeighbor(ready, verified, verified.Challenge, memoryTestSnapshot(), state))
	state = memoryTestSnapshot()
	state.Current = memoryNeighborHeld - 1
	require.Error(t, validateMemoryNeighbor(ready, verified, verified.Challenge, memoryTestSnapshot(), state))
}

func memoryTestHost() memoryHost {
	first := int64(1)
	return memoryHost{BootID: "boot", Kernel: "kernel", FirstSequence: &first, LastSequence: 100,
		Available: 4 * memoryJobLimit, Processes: map[string]memoryHostProcess{
			"10": {Command: "kubelet", StartTicks: "1"}, "11": {Command: "slurmd", StartTicks: "2"},
			"12": {Command: "nvidia-hostengine", StartTicks: "3"},
		}}
}

func memoryTestHostAfter() memoryHost {
	after := memoryTestHost()
	after.LastSequence = 102
	after.OOMMessages = []memoryKernelMessage{
		{Sequence: 101, Message: "oom-kill:constraint=CONSTRAINT_MEMCG,oom_memcg=/slurm/job_42,task_memcg=/slurm/job_42/step_0/user,task=python3,pid=300"},
		{Sequence: 102, Message: "Memory cgroup out of memory: Killed process 300 (python3) total-vm:1234kB"},
	}
	return after
}

func TestMemoryHostRequiresCompleteAttributedOOMAndStableServices(t *testing.T) {
	before := memoryTestHost()
	require.NoError(t, validateMemoryHostBaseline(before))
	for _, change := range []func(*memoryHost){
		func(h *memoryHost) { h.BootID = "new boot" },
		func(h *memoryHost) {
			h.Processes["12"] = memoryHostProcess{Command: "nvidia-hostengine", StartTicks: "new process"}
		},
		func(h *memoryHost) { delete(h.Processes, "10") },
		func(h *memoryHost) { first := int64(102); h.FirstSequence = &first },
		func(h *memoryHost) { h.OOMMessages[0].Sequence = 99 },
		func(h *memoryHost) {
			h.OOMMessages[0].Message = strings.ReplaceAll(h.OOMMessages[0].Message, "job_42", "job_420")
		},
		func(h *memoryHost) {
			h.OOMMessages[0].Message = "oom-kill:constraint=CONSTRAINT_NONE,global_oom,task=kubelet"
		},
		func(h *memoryHost) { h.OOMMessages[1].Message = "Out of memory: Killed process 10 (kubelet)" },
		func(h *memoryHost) { h.OOMMessages = h.OOMMessages[1:] },
		func(h *memoryHost) { h.OOMMessages = h.OOMMessages[:1] },
	} {
		after := memoryTestHostAfter()
		require.NoError(t, validateMemoryHost(before, after, memoryTestSnapshot().Cgroup))
		change(&after)
		require.Error(t, validateMemoryHost(before, after, memoryTestSnapshot().Cgroup))
	}
	before.Available = memoryJobLimit
	require.Error(t, validateMemoryHostBaseline(before))
}

func memoryTestHealth() memoryHealth {
	return memoryHealth{
		Pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod"}, Spec: corev1.PodSpec{NodeName: "host"}, Status: corev1.PodStatus{
			Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "slurmd", ContainerID: "container", Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
			InitContainerStatuses: []corev1.ContainerStatus{{Name: "munge", ContainerID: "munge"}},
		}},
		Node: corev1.Node{ObjectMeta: metav1.ObjectMeta{UID: "node"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue}, {Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
		}}},
	}
}

func TestMemoryHostRootSupportsCPUAndGPUWorkers(t *testing.T) {
	for _, source := range []string{"/", "/run/nvidia/driver"} {
		pod := corev1.Pod{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "slurmd", VolumeMounts: []corev1.VolumeMount{{Name: "host", MountPath: "/host-root"}}}},
			Volumes:    []corev1.Volume{{Name: "host", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: source}}}},
		}}
		root, err := memoryHostRoot(pod)
		require.NoError(t, err)
		assert.Equal(t, "/host-root", root)
		pod.Spec.Volumes[0].HostPath.Path = "/unrelated"
		_, err = memoryHostRoot(pod)
		require.Error(t, err)
	}
}

func TestMemoryHealthDetectsReplacementRestartsAndPressure(t *testing.T) {
	before := memoryTestHealth()
	for _, change := range []func(*memoryHealth){
		func(h *memoryHealth) { h.Pod.UID = "new pod" },
		func(h *memoryHealth) { h.Node.UID = "new host" },
		func(h *memoryHealth) { h.Pod.Status.ContainerStatuses[0].RestartCount++ },
		func(h *memoryHealth) { h.Pod.Status.ContainerStatuses[0].ContainerID = "new container" },
		func(h *memoryHealth) { h.Pod.Status.ContainerStatuses[0].Ready = false },
		func(h *memoryHealth) { h.Pod.Status.InitContainerStatuses[0].RestartCount++ },
		func(h *memoryHealth) { h.Node.Status.Conditions[1].Status = corev1.ConditionTrue },
		func(h *memoryHealth) { h.Pod.Status.Conditions[0].Status = corev1.ConditionFalse },
	} {
		after := memoryTestHealth()
		require.NoError(t, validateMemoryHealth(before, after))
		change(&after)
		require.Error(t, validateMemoryHealth(before, after))
	}
}

func TestMemoryJobCommandsQuotePathsAndPreserveOOMExit(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory ' $(touch escaped)")
	require.NoError(t, os.Mkdir(directory, 0o755))
	for _, execution := range []string{"native", "Enroot", "Docker"} {
		command := "SLURM_JOB_ID=42\npython3() { return 0; }\ntimeout() { return 0; }\nsrun() { printf '<%s>\\n' \"$@\"; return 137; }\n" +
			memoryJobCommand(directory, "target", execution, "owned-label")
		output, err := exec.CommandContext(t.Context(), "bash", "-c", command).CombinedOutput()
		require.ErrorContains(t, err, "exit status 137", string(output))
		assert.Contains(t, string(output), "<"+filepath.Join(directory, "probe.py")+">")
		assert.Contains(t, string(output), "<"+directory+">")
		switch execution {
		case "Enroot":
			assert.Contains(t, string(output), "<--container-mounts="+directory+":"+directory+">")
		case "Docker":
			assert.Contains(t, string(output), "<e2e-memory-42>")
			assert.NotContains(t, string(output), "<--memory", "Docker must exercise the Slurm limit")
		}
		exit, err := os.ReadFile(filepath.Join(directory, "target.exit"))
		require.NoError(t, err)
		assert.Equal(t, "137", string(exit))
	}
}

func TestMemoryProbeUsesFiniteAllocationAndIndependentOOMCounters(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	file := filepath.Join(t.TempDir(), "probe.py")
	require.NoError(t, os.WriteFile(file, []byte(memoryProbe), 0o600))
	harness := `import importlib.util, json, pathlib, sys, tempfile, unittest
from unittest import mock
spec = importlib.util.spec_from_file_location("probe", sys.argv[1])
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)
class ProbeTests(unittest.TestCase):
    def test_finite_ceiling_without_enforcement(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "target.start").touch()
            (root / "target.exceed").touch()
            with mock.patch.object(probe, "MIB", 1024), mock.patch.object(probe, "identity", return_value={"pid": 1}), mock.patch.object(probe.time, "sleep"):
                self.assertEqual(probe.target(root, 8192), 42)
            self.assertEqual(json.loads((root / "target.bypassed.json").read_text())["allocated"], 10240)

    def observation(self, action):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "memory.current").write_text("123")
            (root / "memory.events").write_text("oom 0\noom_kill 0\nmax 0\n")
            original = probe.publish
            def publish(filename, result):
                original(filename, result)
                if not result["finished"] and action == "kill":
                    (root / "memory.events").write_text("oom 1\noom_kill 1\nmax 1\n")
                if not result["finished"] and action == "remove":
                    (root / "memory.current").unlink()
            state = {"cgroup": str(root), "current": 123, "events": {"oom": 0, "oom_kill": 0, "max": 0}}
            with mock.patch.object(probe, "snapshot", return_value=state), mock.patch.object(probe, "publish", side_effect=publish):
                probe.observe(root, root, 0 if action == "timeout" else 1)
            return json.loads((root / "observer.json").read_text())

    def test_oom_does_not_mutate_baseline(self):
        result = self.observation("kill")
        self.assertEqual(result["before"]["events"]["oom_kill"], 0)
        self.assertEqual(result["after"]["events"]["oom_kill"], 1)
        self.assertTrue(result["finished"])
        self.assertFalse(result["timed_out"])

    def test_timeout_is_not_an_oom(self):
        result = self.observation("timeout")
        self.assertTrue(result["timed_out"])
        self.assertEqual(result["after"]["events"]["oom_kill"], 0)

    def test_removal_is_not_an_oom(self):
        result = self.observation("remove")
        self.assertTrue(result["removed"])
        self.assertEqual(result["after"]["events"]["oom_kill"], 0)

    def test_swap_limit_inheritance(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            group = root / "job"
            group.mkdir()
            (group / "memory.max").write_text("1073741824")
            (group / "memory.current").write_text("123")
            (group / "memory.events").write_text("oom 0\noom_kill 0\nmax 0\n")
            (group / "memory.swap.max").write_text("max")
            (root / "memory.swap.max").write_text("0")
            (root / "swaps").write_text("Filename Type Size Used Priority\n/swap file 1024 0 -2\n")
            result = probe.snapshot(group, root, root / "swaps")
            self.assertTrue(result["swap"])
            self.assertIn("0", result["swap_limits"].values())

    def test_locates_namespaced_process_without_trusting_its_pid(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            work = root / "work"
            work.mkdir()
            proc = root / "proc"
            proc.mkdir()
            def process(pid, started):
                entry = proc / str(pid)
                entry.mkdir()
                (entry / "cmdline").write_bytes("\0".join(["python3", "-u", str(work / "probe.py"), "target", str(work), "1073741824", ""]).encode())
                (entry / "stat").write_text(str(pid) + " (python3) " + " ".join(["S"] + ["0"] * 18 + [started]))
                (entry / "cgroup").write_text("0::/slurm/job_42/step_0/user/task_0\n")
                return entry
            process(123, "456")
            reused = process(124, "999")
            result = probe.locate(work, "target", "456", proc)
            self.assertEqual(result["pid"], 123)
            self.assertIn("/job_42/", result["task_cgroup"])
            (reused / "stat").write_text("124 (python3) " + " ".join(["S"] + ["0"] * 18 + ["456"]))
            with self.assertRaises(AssertionError):
                probe.locate(work, "target", "456", proc)

sys.argv = [sys.argv[0]]
unittest.main()
`
	output, err := exec.CommandContext(t.Context(), python, "-B", "-c", harness, file).CombinedOutput()
	require.NoError(t, err, string(output))
}

type memoryCleanupRuntime struct {
	framework.Runtime
	commands []string
	err      error
}

type memoryStartupRuntime struct {
	framework.Runtime
	commands   []string
	setupState string
}

func (r *memoryStartupRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		r.commands = append(r.commands, command)
		switch {
		case strings.Contains(command, "sbatch "):
			return "45", nil
		case strings.HasPrefix(command, "squeue "):
			if r.setupState == "" {
				return "RUNNING", nil
			}
		case strings.HasPrefix(command, "sacct "):
			return "45|" + r.setupState + "|0:0", nil
		case strings.HasPrefix(command, "scontrol -o show job "):
			return "Restarts=0", nil
		case strings.Contains(command, "target.exit"):
			return "1", nil
		}
		return "", nil
	})
}

func (*memoryStartupRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	done, err := condition(ctx)
	if err == nil && !done {
		return errors.New("condition pending")
	}
	return err
}

func TestMemoryPreparesEnrootImageOutsideMeasuredJob(t *testing.T) {
	for _, state := range []string{"COMPLETED", "OUT_OF_MEMORY"} {
		runtime := &memoryStartupRuntime{setupState: state}
		s := NewMemoryIsolation(nil, runtime, framework.NewSlurmClient(runtime), nil, nil)
		s.worker = framework.WorkerInfo{Name: "worker-0", SlurmNode: framework.SlurmNodeInfo{RealMemoryMiB: 8192}}
		s.report = map[string]any{}
		err := s.prepareEnrootImage(t.Context())
		if state == "COMPLETED" {
			require.NoError(t, err)
			assert.True(t, s.imageSetupJob.IsZero())
		} else {
			require.ErrorContains(t, err, "OUT_OF_MEMORY")
			assert.Equal(t, "45", s.imageSetupJob.ID, "failed setup remains owned for cleanup")
		}
		command := strings.Join(runtime.commands, "\n")
		assert.Contains(t, command, "--mem=4G")
		assert.Contains(t, command, "ENROOT_MAX_PROCESSORS=2")
		assert.Contains(t, command, "python3 --version")
		assert.Equal(t, int64(1073741824), memoryJobLimit)
	}
}

func TestMemoryStartupExitIsReportedBeforeProbeTimeout(t *testing.T) {
	runtime := &memoryStartupRuntime{}
	s := NewMemoryIsolation(nil, runtime, framework.NewSlurmClient(runtime), nil, nil)
	s.workdir = "/artifacts/memory"
	var ready memoryWorkload
	err := s.waitProbe(t.Context(), framework.SbatchJob{ID: "45"}, "target.ready.json", &ready, time.Minute)
	require.ErrorContains(t, err, "start memory workload before target.ready.json: exit=1")
	var permanent *framework.PermanentError
	require.ErrorAs(t, err, &permanent)
}

func (r *memoryCleanupRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(ctx context.Context, command string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r.commands = append(r.commands, command)
		if strings.HasPrefix(command, "scancel ") {
			return "", r.err
		}
		if strings.HasPrefix(command, "docker ps ") {
			return "owned-container\n", nil
		}
		return "", nil
	})
}
func (r *memoryCleanupRuntime) Worker(framework.WorkerInfo) framework.CommandScope { return r.Jail() }
func (*memoryCleanupRuntime) Logf(string, ...any)                                  {}
func (*memoryCleanupRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	done, err := condition(ctx)
	if err == nil && !done {
		return errors.New("condition pending")
	}
	return err
}

func TestMemoryCleanupSurvivesCancellationAndReportsFailure(t *testing.T) {
	for _, failure := range []error{nil, assert.AnError} {
		runtime := &memoryCleanupRuntime{err: failure}
		s := NewMemoryIsolation(nil, runtime, framework.NewSlurmClient(runtime), nil, nil)
		s.worker = framework.WorkerInfo{Name: "worker-0"}
		s.workdir = "/artifacts/memory"
		s.execution = "Docker"
		s.containerLabel = "owned-label"
		s.jobs = []memoryJob{{SbatchJob: framework.SbatchJob{ID: "42"}}, {SbatchJob: framework.SbatchJob{ID: "43"}}}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := cleanupStepFamily(ctx, s)
		if failure == nil {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, failure.Error())
		}
		commands := strings.Join(runtime.commands, "\n")
		assert.Contains(t, commands, "scancel '42'")
		assert.Contains(t, commands, "scancel '43'")
		assert.Contains(t, commands, "observer.stop")
		assert.Contains(t, commands, "label=soperator.e2e.memory=owned-label")
		assert.Contains(t, commands, "docker rm -f 'owned-container'")
		assert.Empty(t, s.jobs)
		assert.Empty(t, s.workdir)
		count := len(runtime.commands)
		require.NoError(t, cleanupStepFamily(t.Context(), s))
		assert.Len(t, runtime.commands, count, "cleanup must be idempotent")
	}
}
