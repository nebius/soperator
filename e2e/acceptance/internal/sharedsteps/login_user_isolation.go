package sharedsteps

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	loginMemoryFirstUser   = "soperatormema"
	loginMemorySecondUser  = "soperatormemb"
	loginMemoryHigh        = int64(480 * 1024 * 1024)
	loginMemoryMax         = int64(512 * 1024 * 1024)
	loginMemoryHeld        = int64(128 * 1024 * 1024)
	loginMemoryHold        = 5 * time.Second
	loginMemoryRollout     = 15 * time.Minute
	loginMemoryHighTimeout = 2 * time.Minute
	loginMemoryHardTimeout = 5 * time.Minute
	loginMemoryCommand     = loginMemoryHardTimeout + 30*time.Second
	loginUpdateAnnounced   = "slurm.nebius.ai/login-update-announced-at"
	loginUpdateScheduled   = "slurm.nebius.ai/login-update-scheduled-at"
	loginUpdateRequested   = "slurm.nebius.ai/login-update-requested"
)

//go:embed login_memory_probe.py
var loginMemoryProbe string

type loginMemorySnapshot struct {
	Cgroup   string           `json:"cgroup"`
	Current  int64            `json:"current"`
	High     string           `json:"high"`
	Max      string           `json:"max"`
	SwapMax  *string          `json:"swap_max"`
	Events   map[string]int64 `json:"events"`
	Pressure *string          `json:"pressure"`
	Peak     *int64           `json:"peak"`
}

type loginMemoryEnvironment struct {
	Swap         bool              `json:"swap"`
	SwapDisabled bool              `json:"swap_disabled"`
	SwapLimits   map[string]string `json:"swap_limits"`
}

type loginMemorySession struct {
	loginMemoryEnvironment
	Groups   []string            `json:"groups"`
	UID      int64               `json:"uid"`
	Hostname string              `json:"hostname"`
	Kernel   string              `json:"kernel"`
	Session  string              `json:"session"`
	Memory   loginMemorySnapshot `json:"memory"`
}

type loginMemoryWorkload struct {
	Before      loginMemorySnapshot `json:"before"`
	After       loginMemorySnapshot `json:"after"`
	ReturnCode  int                 `json:"returncode"`
	TimedOut    bool                `json:"timed_out"`
	Stderr      string              `json:"stderr"`
	SampledPeak int64               `json:"sampled_peak"`
	Allocation  struct {
		HeldSeconds   float64             `json:"held_seconds"`
		Samples       []loginMemorySample `json:"samples"`
		Held          loginMemorySnapshot `json:"held"`
		SampledPeak   int64               `json:"sampled_peak"`
		LimitBypassed bool                `json:"limit_bypassed"`
	} `json:"allocation"`
}

type loginMemorySample struct {
	Current        int64   `json:"current"`
	HighEvents     int64   `json:"high_events"`
	ElapsedSeconds float64 `json:"elapsed_seconds"`
}

type loginMemoryServices struct {
	ContainerLocal map[string]int64 `json:"container_local"`
	Container      map[string]int64 `json:"container"`
	Target         map[string]int64 `json:"target"`
	Init           map[string]int64 `json:"init"`
}

type loginMemoryRolloutSnapshot struct {
	StatefulSet kubeobjects.LoginStatefulSet
	Pods        []corev1.Pod
}

type loginMemoryAutoscaling struct {
	Size            int32
	Configuration   json.RawMessage
	StatefulSetName string
}

type loginMemoryNeighbor struct {
	Ready   *bool               `json:"ready"`
	Error   string              `json:"error"`
	Memory  loginMemorySnapshot `json:"memory"`
	Process struct {
		UID int64 `json:"uid"`
	} `json:"process"`
}

type loginMemoryContainer struct {
	RestartCount    int32                            `json:"restart_count"`
	LastTermination *corev1.ContainerStateTerminated `json:"last_termination,omitempty"`
}

type loginMemoryPod struct {
	UID        string                          `json:"uid"`
	Phase      corev1.PodPhase                 `json:"phase"`
	Ready      bool                            `json:"ready"`
	Deleting   bool                            `json:"deleting"`
	Containers map[string]loginMemoryContainer `json:"containers"`
}

type LoginUserIsolation struct {
	loginWorkload
	originalLimits      map[string]any
	originalAutoscaling *loginMemoryAutoscaling
	originalConfig      string
	frozenReplicas      int32
	workdir             string
	artifactDir         string
	report              map[string]any
	pods                map[string]loginMemoryPod
	sessions            [2]loginMemorySession
	neighbor            loginMemorySnapshot
	services            loginMemoryServices
	high, hard          loginMemoryWorkload
}

func NewLoginUserIsolation(info *framework.ClusterInfo, runtime framework.Runtime, kubectl *framework.KubectlClient) *LoginUserIsolation {
	return &LoginUserIsolation{loginWorkload: loginWorkload{info: info, runtime: runtime, kubectl: kubectl}}
}

func (s *LoginUserIsolation) RegisterSteps(sc *godog.ScenarioContext) {
	steps := []struct {
		expression string
		run        func(context.Context) error
	}{
		{`^login user isolation is configured with explicit per-user memory limits$`, s.configureMemory},
		{`^two regular users have SSH sessions on the same login node$`, s.prepareMemoryUsers},
		{`^both users' SSH sessions are limited by the explicit memory limits$`, s.checkMemorySessions},
		{`^the second user holds memory in a background process on the login node$`, s.holdNeighborMemory},
		{`^the first user allocates memory above the per-user throttling threshold$`, s.allocateAboveHigh},
		{`^the first user's allocation is throttled but not killed$`, s.checkMemoryHigh},
		{`^the first user allocates memory beyond the per-user hard limit$`, s.allocateAboveMax},
		{`^the first user's allocation is OOM-killed inside the first user's cgroup$`, s.checkMemoryOOM},
		{`^the second user's background process keeps running with its memory intact$`, s.checkNeighborMemory},
		{`^the second user can still SSH to the login node, allocate memory and run Slurm commands$`, s.checkNeighborSSH},
		{`^login pods and services were not affected by the memory exhaustion$`, s.checkMemoryServices},
	}
	for _, step := range steps {
		sc.Step(step.expression, func(ctx context.Context) error {
			err := step.run(ctx)
			if s.report != nil {
				result := "passed"
				if err != nil {
					result = err.Error()
				}
				s.record(step.expression, result)
				if err != nil {
					s.writeReport()
				}
			}
			return err
		})
	}
}

func (s *LoginUserIsolation) configureMemory(ctx context.Context) error {
	var cluster kubeobjects.SlurmCluster
	if err := s.kubectl.GetJSON(ctx, &cluster, "get", "slurmcluster", s.info.SlurmClusterName,
		"-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return fmt.Errorf("read login memory isolation configuration: %w", err)
	}
	reason, err := loginMemoryPrerequisite(cluster)
	if err != nil {
		return err
	}
	if reason != "" {
		s.runtime.Logf("Login memory isolation: %s, skipping scenario", reason)
		return godog.ErrSkip
	}
	if artifacts, ok := framework.ScenarioArtifacts(ctx); ok {
		s.artifactDir = artifacts.RunnerDir
	}
	login := cluster.Spec.SlurmNodes.Login
	s.report = map[string]any{
		"soperator_version": s.info.TargetSoperatorVersion, "docker_enabled": login.Docker.Enabled,
		"original_user_isolation": login.UserIsolation, "original_autoscaling": login.Autoscaling, "original_size": login.Size,
		"memory_high": loginMemoryHigh, "memory_max": loginMemoryMax, "held_bytes": loginMemoryHeld,
		"hold_seconds": loginMemoryHold.Seconds(), "high_timeout_seconds": loginMemoryHighTimeout.Seconds(),
		"hard_timeout_seconds": loginMemoryHardTimeout.Seconds(), "ssh_timeout_seconds": loginMemoryCommand.Seconds(),
	}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range build.Settings {
			if setting.Key == "vcs.revision" || setting.Key == "vcs.modified" {
				s.report[setting.Key] = setting.Value
			}
		}
	}
	state, err := s.waitMemoryRollout(ctx, "", false, nil)
	if err != nil {
		return err
	}
	if err := s.installMemoryProbe(ctx); err != nil {
		return err
	}
	if err := s.checkMemoryEnvironment(ctx, "environment_before"); err != nil {
		return err
	}
	s.originalConfig, err = s.mountedMemoryConfig(ctx, state.Pods[0].Name)
	if err != nil {
		return err
	}
	enabled, err := loginFeatureEnabled(login.Autoscaling)
	if err != nil {
		return err
	}
	if enabled {
		s.originalAutoscaling = &loginMemoryAutoscaling{Size: login.Size,
			Configuration: append(json.RawMessage(nil), login.Autoscaling...), StatefulSetName: state.StatefulSet.Metadata.Name}
		frozen := *state.StatefulSet.Spec.Replicas
		s.frozenReplicas = frozen
		if err := s.patchLogin(ctx, map[string]any{"size": frozen, "autoscaling": map[string]any{"enabled": false}}); err != nil {
			return err
		}
		if err := s.waitLoginHPA(ctx, state.StatefulSet.Metadata.Name, false, loginMemoryRollout); err != nil {
			return err
		}
		state, err = s.waitMemoryRollout(ctx, "", false, nil)
		if err != nil {
			return err
		}
		s.record("frozen_replicas", frozen)
	}
	limits, err := loginMemoryRestorePatch(login.UserIsolation)
	if err != nil {
		return err
	}
	// Capture restoration before patching: a command can time out after the API accepted it.
	s.originalLimits = limits
	if err := s.patchLogin(ctx, map[string]any{"userIsolation": map[string]any{
		"memoryHigh": resource.NewQuantity(loginMemoryHigh, resource.BinarySI).String(),
		"memoryMax":  resource.NewQuantity(loginMemoryMax, resource.BinarySI).String(),
	}}); err != nil {
		return err
	}
	state, err = s.waitMemoryRollout(ctx, state.StatefulSet.Status.UpdateRevision, !loginMemoryLimitsMatch(limits), nil)
	if err != nil {
		return err
	}
	s.pods = snapshotLoginMemoryPods(state.Pods)
	s.record("pods_before", s.pods)
	return nil
}

func (s *LoginUserIsolation) checkMemoryEnvironment(ctx context.Context, reportKey string) error {
	var environment loginMemoryEnvironment
	if err := s.rootProbe(ctx, "environment", reportKey, &environment); err != nil {
		return err
	}
	if environment.Swap && !environment.SwapDisabled {
		s.runtime.Logf("Login memory isolation: swap is available in the cgroup hierarchy, skipping scenario: %v", environment.SwapLimits)
		return godog.ErrSkip
	}
	return nil
}

func loginMemoryPrerequisite(cluster kubeobjects.SlurmCluster) (string, error) {
	login := cluster.Spec.SlurmNodes.Login
	if login.Size == 0 {
		return "login nodes are disabled", nil
	}
	enabled, err := loginFeatureEnabled(login.UserIsolation)
	if err != nil {
		return "", err
	}
	if !enabled {
		return "login user isolation is disabled", nil
	}
	if memory, exists := login.Sshd.Resources[corev1.ResourceMemory]; exists && memory.Value() < 2*1024*1024*1024 {
		return "SSHD memory limit is below 2Gi", nil
	}
	return "", nil
}

func loginMemoryRestorePatch(raw json.RawMessage) (map[string]any, error) {
	var original map[string]any
	if err := json.Unmarshal(raw, &original); err != nil {
		return nil, fmt.Errorf("decode original login memory limits: %w", err)
	}
	return map[string]any{"memoryHigh": original["memoryHigh"], "memoryMax": original["memoryMax"]}, nil
}

func loginMemoryLimitsMatch(limits map[string]any) bool {
	for key, expected := range map[string]int64{"memoryHigh": loginMemoryHigh, "memoryMax": loginMemoryMax} {
		text, ok := limits[key].(string)
		if !ok {
			return false
		}
		quantity, err := resource.ParseQuantity(text)
		if err != nil || quantity.Value() != expected {
			return false
		}
	}
	return true
}

func (s *LoginUserIsolation) waitMemoryRollout(ctx context.Context, previous string, changed bool, checkPod func(context.Context, corev1.Pod) error) (loginMemoryRolloutSnapshot, error) {
	var observed loginMemoryRolloutSnapshot
	err := s.runtime.WaitFor(ctx, "login memory configuration rollout", loginMemoryRollout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			sts, err := s.loginStatefulSet(waitCtx)
			if err != nil {
				return false, err
			}
			pods, err := s.loginPods(waitCtx)
			if err != nil {
				return false, err
			}
			if sts.Status.ObservedGeneration >= sts.Metadata.Generation && sts.Status.UpdateRevision != "" &&
				(!changed || sts.Status.UpdateRevision != previous) {
				if err := s.requestMemoryPodUpdates(waitCtx, sts, pods); err != nil {
					return false, err
				}
			}
			if err := loginMemoryRolloutReady(sts, pods, previous, changed); err != nil {
				return false, err
			}
			if s.frozenReplicas > 0 && *sts.Spec.Replicas != s.frozenReplicas {
				return false, fmt.Errorf("wait for frozen login replicas: got %d, expected %d", *sts.Spec.Replicas, s.frozenReplicas)
			}
			if checkPod != nil {
				for _, pod := range pods {
					if err := checkPod(waitCtx, pod); err != nil {
						return false, err
					}
				}
			}
			observed = loginMemoryRolloutSnapshot{StatefulSet: sts, Pods: pods}
			return true, nil
		})
	return observed, err
}

func (s *LoginUserIsolation) requestMemoryPodUpdates(ctx context.Context, sts kubeobjects.LoginStatefulSet, pods []corev1.Pod) error {
	if sts.Spec.UpdateStrategy.Type != string(appsv1.OnDeleteStatefulSetStrategyType) {
		return nil
	}
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil || !kubeobjects.PodReady(pod) ||
			pod.Labels[appsv1.StatefulSetRevisionLabel] == sts.Status.UpdateRevision ||
			pod.Annotations[loginUpdateAnnounced] == "" || pod.Annotations[loginUpdateScheduled] == "" ||
			pod.Annotations[loginUpdateRequested] == string(pod.UID) {
			continue
		}
		output, err := s.runtime.Kubectl().Run(ctx, "exec", "-n", framework.SoperatorNamespace, pod.Name,
			"-c", loginSSHDContainer, "--", "chroot", "/mnt/jail", "soperator-login-restart", "--yes")
		if err != nil {
			return fmt.Errorf("request early memory configuration restart for login pod %s (uid=%s): %w", pod.Name, pod.UID, err)
		}
		s.runtime.Logf("Requested early restart for login pod %s (uid=%s): %s", pod.Name, pod.UID, strings.TrimSpace(output))
	}
	return nil
}

func loginMemoryRolloutReady(sts kubeobjects.LoginStatefulSet, pods []corev1.Pod, previous string, changed bool) error {
	status := sts.Status
	// OnDelete preserves currentRevision even after every pod reaches updateRevision.
	currentRevisionReady := sts.Spec.UpdateStrategy.Type == string(appsv1.OnDeleteStatefulSetStrategyType) ||
		status.CurrentRevision == status.UpdateRevision
	if sts.Spec.Replicas == nil || *sts.Spec.Replicas < 1 || status.UpdateRevision == "" ||
		(changed && status.UpdateRevision == previous) || status.ObservedGeneration < sts.Metadata.Generation ||
		!currentRevisionReady || status.UpdatedReadyReplicas != *sts.Spec.Replicas ||
		status.ReadyReplicas != *sts.Spec.Replicas || int32(len(pods)) != *sts.Spec.Replicas {
		return fmt.Errorf("wait for login rollout: previous=%s changed=%t spec=%v generation=%d status=%+v pods=%d",
			previous, changed, replicaValue(sts.Spec.Replicas), sts.Metadata.Generation, status, len(pods))
	}
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || !kubeobjects.PodReady(pod) ||
			pod.Labels["controller-revision-hash"] != status.UpdateRevision {
			return fmt.Errorf("wait for login pod %s: phase=%s ready=%t revision=%s, expected %s",
				pod.Name, pod.Status.Phase, kubeobjects.PodReady(pod), pod.Labels["controller-revision-hash"], status.UpdateRevision)
		}
	}
	return nil
}

func (s *LoginUserIsolation) installMemoryProbe(ctx context.Context) error {
	if s.workdir == "" {
		s.workdir = fmt.Sprintf("/tmp/soperator-e2e-login-memory-%d", time.Now().UnixNano())
	}
	command := "install -d -m 0755 " + framework.ShellQuote(s.workdir) +
		" && printf %s " + framework.ShellQuote(loginMemoryProbe) + " > " + framework.ShellQuote(path.Join(s.workdir, "probe.py")) +
		" && chmod 0644 " + framework.ShellQuote(path.Join(s.workdir, "probe.py"))
	if _, err := s.runtime.Jail().Run(ctx, command); err != nil {
		return fmt.Errorf("install login memory probe: %w", err)
	}
	return nil
}

func (s *LoginUserIsolation) prepareMemoryUsers(ctx context.Context) error {
	if err := s.installMemoryProbe(ctx); err != nil {
		return err
	}
	if err := s.checkMemoryEnvironment(ctx, "environment_after_rollout"); err != nil {
		return err
	}
	for i, user := range []string{loginMemoryFirstUser, loginMemorySecondUser} {
		if err := ensureUnprivilegedSSHTestUser(ctx, s.runtime, user); err != nil {
			return err
		}
		if err := s.sshProbe(ctx, user, "inspect", user+"_session_before", &s.sessions[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *LoginUserIsolation) checkMemorySessions(context.Context) error {
	for _, session := range s.sessions {
		if err := validateLoginMemorySession(session); err != nil {
			return err
		}
		s.runtime.Logf("Login memory isolation: uid=%d kernel=%s cgroup=%s memory.peak=%t swap=%t swap_accounting=%t PSI=%t",
			session.UID, session.Kernel, session.Memory.Cgroup, session.Memory.Peak != nil, session.Swap,
			session.Memory.SwapMax != nil, session.Memory.Pressure != nil)
	}
	a, b := s.sessions[0], s.sessions[1]
	if a.UID == b.UID || a.Hostname != b.Hostname || a.Memory.Cgroup == b.Memory.Cgroup ||
		path.Dir(a.Memory.Cgroup) != path.Dir(b.Memory.Cgroup) {
		return fmt.Errorf("verify distinct users on the same login node: first=%+v second=%+v", a, b)
	}
	return nil
}

func validateLoginMemorySession(session loginMemorySession) error {
	expected := "user-" + strconv.FormatInt(session.UID, 10)
	if session.UID < 1000 || session.Hostname == "" || session.Kernel == "" || path.Base(session.Memory.Cgroup) != expected ||
		path.Base(path.Dir(session.Memory.Cgroup)) != "users" ||
		(session.Session != session.Memory.Cgroup && session.Session != path.Join(session.Memory.Cgroup, "sessions")) {
		return fmt.Errorf("verify login SSH user cgroup: %+v", session)
	}
	if session.Memory.High != strconv.FormatInt(loginMemoryHigh, 10) || session.Memory.Max != strconv.FormatInt(loginMemoryMax, 10) {
		return fmt.Errorf("verify login memory limits: uid=%d high=%s max=%s, expected %d/%d",
			session.UID, session.Memory.High, session.Memory.Max, loginMemoryHigh, loginMemoryMax)
	}
	if session.Swap && !session.SwapDisabled {
		return fmt.Errorf("verify login swap prerequisite: uid=%d has usable swap: %v", session.UID, session.SwapLimits)
	}
	if err := validateUnprivilegedSSHGroups(session.Groups); err != nil {
		return err
	}
	return validateLoginMemoryEvents(session.Memory.Events)
}

func validateLoginMemoryEvents(events map[string]int64) error {
	for _, key := range []string{"high", "max", "oom", "oom_kill"} {
		if value, exists := events[key]; !exists || value < 0 {
			return fmt.Errorf("read login memory.events: missing or invalid %s in %v", key, events)
		}
	}
	return nil
}

func (s *LoginUserIsolation) holdNeighborMemory(ctx context.Context) error {
	directory := path.Join(s.workdir, loginMemorySecondUser)
	prepare := "install -d -m 0700 -o " + framework.ShellQuote(loginMemorySecondUser) + " -- " + framework.ShellQuote(directory)
	if _, err := s.runtime.Jail().Run(ctx, prepare); err != nil {
		return fmt.Errorf("prepare neighboring login memory directory %s: %w", directory, err)
	}
	command := "nohup setsid " + s.probeCommand("hold", directory) +
		" >" + framework.ShellQuote(path.Join(directory, "hold.log")) + " 2>&1 </dev/null &"
	if _, err := runSSHCommand(ctx, s.runtime, loginMemorySecondUser, "id_ecdsa", "localhost", loginMemoryCommand, command); err != nil {
		return fmt.Errorf("start neighboring login memory holder: %w", err)
	}
	if err := s.runtime.WaitFor(ctx, "neighboring user holds 128Mi", time.Minute, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			_, err := s.runtime.Jail().Run(waitCtx, "test -s "+framework.ShellQuote(path.Join(directory, "ready")))
			return err == nil, err
		}); err != nil {
		diagnosticsCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		logPath := framework.ShellQuote(path.Join(directory, "hold.log"))
		diagnostics, diagnosticsErr := s.runtime.Jail().Run(diagnosticsCtx,
			"ls -ld -- "+framework.ShellQuote(s.workdir)+" "+framework.ShellQuote(directory)+
				" 2>&1; if [ -f "+logPath+" ]; then tail -n 50 -- "+logPath+" 2>&1; fi")
		s.record("neighbor_holder_diagnostics", diagnostics)
		if diagnosticsErr != nil {
			return fmt.Errorf("%w; collect neighboring memory holder diagnostics: %v; output: %s", err, diagnosticsErr, diagnostics)
		}
		return fmt.Errorf("%w; neighboring memory holder diagnostics:\n%s", err, diagnostics)
	}
	var session loginMemorySession
	if err := s.sshProbe(ctx, loginMemorySecondUser, "inspect", "neighbor_before", &session); err != nil {
		return err
	}
	s.neighbor = session.Memory
	if s.neighbor.Current < loginMemoryHeld {
		return fmt.Errorf("verify neighboring memory holder: current=%d, expected at least %d", s.neighbor.Current, loginMemoryHeld)
	}
	return s.rootProbe(ctx, "services", "services_before", &s.services, s.containerCgroup(), s.sessions[0].Memory.Cgroup)
}

func (s *LoginUserIsolation) allocateAboveHigh(ctx context.Context) error {
	return s.sshProbe(ctx, loginMemoryFirstUser, "high", "high", &s.high)
}

func (s *LoginUserIsolation) checkMemoryHigh(context.Context) error {
	s.record("high_event_deltas", loginMemoryEventDeltas(s.high.Before.Events, s.high.After.Events))
	return validateLoginMemoryWorkload(s.high, false)
}

func (s *LoginUserIsolation) allocateAboveMax(ctx context.Context) error {
	return s.sshProbe(ctx, loginMemoryFirstUser, "hard", "hard", &s.hard)
}

func (s *LoginUserIsolation) checkMemoryOOM(context.Context) error {
	s.record("hard_event_deltas", loginMemoryEventDeltas(s.hard.Before.Events, s.hard.After.Events))
	return validateLoginMemoryWorkload(s.hard, true)
}

func validateLoginMemoryWorkload(result loginMemoryWorkload, hard bool) error {
	if err := validateLoginMemoryEvents(result.Before.Events); err != nil {
		return err
	}
	if err := validateLoginMemoryEvents(result.After.Events); err != nil {
		return err
	}
	if result.Before.Cgroup == "" || result.Before.Cgroup != result.After.Cgroup || result.TimedOut || result.Allocation.LimitBypassed {
		return fmt.Errorf("verify login memory workload (hard=%t): %+v", hard, result)
	}
	before, after := result.Before.Events, result.After.Events
	if hard {
		if result.ReturnCode != -9 || after["oom"] <= before["oom"] || after["oom_kill"] <= before["oom_kill"] {
			return fmt.Errorf("verify user-scoped OOM kill: returncode=%d before=%v after=%v stderr=%s",
				result.ReturnCode, before, after, result.Stderr)
		}
		return nil
	}
	observedHigh := false
	elapsed := float64(-1)
	for _, sample := range result.Allocation.Samples {
		if sample.ElapsedSeconds < 0 || sample.ElapsedSeconds < elapsed {
			return fmt.Errorf("verify memory hold samples are ordered: %+v", result.Allocation.Samples)
		}
		elapsed = sample.ElapsedSeconds
		observedHigh = observedHigh || sample.Current > loginMemoryHigh && sample.HighEvents > before["high"]
	}
	if result.ReturnCode != 0 || after["high"] <= before["high"] || after["oom_kill"] != before["oom_kill"] ||
		result.Allocation.Held.Current > loginMemoryMax || result.Allocation.HeldSeconds < loginMemoryHold.Seconds() || len(result.Allocation.Samples) < 2 ||
		result.Allocation.SampledPeak <= loginMemoryHigh || !observedHigh || elapsed < loginMemoryHold.Seconds() {
		return fmt.Errorf("verify user memory throttling without OOM: %+v", result)
	}
	return nil
}

func (s *LoginUserIsolation) checkNeighborMemory(ctx context.Context) error {
	var result loginMemoryNeighbor
	var terminal error
	pollCtx, stop := context.WithCancel(ctx)
	defer stop()
	err := s.runtime.WaitFor(pollCtx, "neighboring login process verifies its memory", time.Minute, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			output, err := s.runtime.Jail().Run(waitCtx, s.probeCommand("survivor", path.Join(s.workdir, loginMemorySecondUser)))
			if err != nil {
				return false, err
			}
			terminal = s.decodeProbe("neighbor_after", output, &result)
			switch {
			case terminal != nil:
			case result.Error != "":
				terminal = fmt.Errorf("verify neighboring login process: %s", result.Error)
			case result.Ready == nil:
				terminal = fmt.Errorf("decode neighboring memory probe: response has neither readiness nor error")
			default:
				return *result.Ready, nil
			}
			// Cancel readiness polling so WaitFor does not retry a definitive error.
			stop()
			return false, terminal
		})
	if terminal != nil {
		return terminal
	}
	if err != nil {
		return err
	}
	if result.Process.UID != s.sessions[1].UID || result.Memory.Cgroup != s.neighbor.Cgroup || result.Memory.Current < loginMemoryHeld {
		return fmt.Errorf("verify neighboring process identity and memory: %+v", result)
	}
	s.record("neighbor_event_deltas", loginMemoryEventDeltas(s.neighbor.Events, result.Memory.Events))
	return loginMemoryEventsUnchanged(s.neighbor.Events, result.Memory.Events, "high", "oom", "oom_kill")
}

func (s *LoginUserIsolation) checkNeighborSSH(ctx context.Context) error {
	var result struct {
		Allocated  int64  `json:"allocated"`
		Partitions string `json:"partitions"`
	}
	if err := s.sshProbe(ctx, loginMemorySecondUser, "smoke", "neighbor_smoke", &result); err != nil {
		return err
	}
	if result.Allocated != loginMemoryHeld || strings.TrimSpace(result.Partitions) == "" {
		return fmt.Errorf("verify neighboring user SSH memory and Slurm commands: %+v", result)
	}
	return nil
}

func (s *LoginUserIsolation) checkMemoryServices(ctx context.Context) error {
	var services loginMemoryServices
	if err := s.rootProbe(ctx, "services", "services_after", &services, s.containerCgroup(), s.sessions[0].Memory.Cgroup); err != nil {
		return err
	}
	s.record("container_event_deltas", loginMemoryEventDeltas(s.services.Container, services.Container))
	s.record("container_local_event_deltas", loginMemoryEventDeltas(s.services.ContainerLocal, services.ContainerLocal))
	s.record("target_event_deltas", loginMemoryEventDeltas(s.services.Target, services.Target))
	s.record("init_event_deltas", loginMemoryEventDeltas(s.services.Init, services.Init))
	if err := validateLoginMemoryServices(s.services, services); err != nil {
		return err
	}
	pods, err := s.loginPods(ctx)
	if err != nil {
		return err
	}
	after := snapshotLoginMemoryPods(pods)
	s.record("pods_after", after)
	return loginMemoryPodsUnchanged(s.pods, after)
}

func validateLoginMemoryServices(before, after loginMemoryServices) error {
	if err := loginMemoryEventsUnchanged(before.ContainerLocal, after.ContainerLocal, "oom", "oom_kill"); err != nil {
		return fmt.Errorf("verify SSHD container local OOM events: %w", err)
	}
	if err := loginMemoryEventsUnchanged(before.Init, after.Init, "oom_kill"); err != nil {
		return fmt.Errorf("verify SSHD init cgroup: %w", err)
	}
	for _, events := range []map[string]int64{before.Container, after.Container, before.Target, after.Target} {
		if err := validateLoginMemoryEvents(events); err != nil {
			return err
		}
	}
	containerKills := after.Container["oom_kill"] - before.Container["oom_kill"]
	targetKills := after.Target["oom_kill"] - before.Target["oom_kill"]
	if targetKills <= 0 || containerKills != targetKills {
		return fmt.Errorf("verify OOM kills are restricted to the target user: container delta=%d target delta=%d", containerKills, targetKills)
	}
	return nil
}

func loginMemoryEventsUnchanged(before, after map[string]int64, keys ...string) error {
	for _, key := range keys {
		initial, initialOK := before[key]
		actual, actualOK := after[key]
		if !initialOK || !actualOK || initial != actual {
			return fmt.Errorf("verify memory.events %s unchanged: before=%v after=%v", key, before, after)
		}
	}
	return nil
}

func loginMemoryEventDeltas(before, after map[string]int64) map[string]int64 {
	deltas := make(map[string]int64, len(after))
	for key, value := range after {
		deltas[key] = value - before[key]
	}
	return deltas
}

func snapshotLoginMemoryPods(pods []corev1.Pod) map[string]loginMemoryPod {
	result := make(map[string]loginMemoryPod, len(pods))
	for _, pod := range pods {
		containers := make(map[string]loginMemoryContainer)
		for _, statuses := range [][]corev1.ContainerStatus{pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses} {
			for _, status := range statuses {
				containers[status.Name] = loginMemoryContainer{
					RestartCount:    status.RestartCount,
					LastTermination: status.LastTerminationState.Terminated.DeepCopy(),
				}
			}
		}
		result[pod.Name] = loginMemoryPod{UID: string(pod.UID), Phase: pod.Status.Phase,
			Ready: kubeobjects.PodReady(pod), Deleting: pod.DeletionTimestamp != nil, Containers: containers}
	}
	return result
}

func loginMemoryPodsUnchanged(before, after map[string]loginMemoryPod) error {
	if len(before) == 0 || len(before) != len(after) {
		return fmt.Errorf("verify login pod identities: before=%v after=%v", before, after)
	}
	for name, pod := range after {
		original, exists := before[name]
		if !exists || pod.UID == "" || pod.UID != original.UID {
			return fmt.Errorf("verify login pod %s identity: before=%+v after=%+v", name, original, pod)
		}
		if pod.Deleting || pod.Phase != corev1.PodRunning || !pod.Ready {
			return fmt.Errorf("verify login pod %s is Running and Ready: %+v", name, pod)
		}
		if len(pod.Containers) != len(original.Containers) {
			return fmt.Errorf("verify login pod %s container statuses: before=%v after=%v", name, original.Containers, pod.Containers)
		}
		for container, status := range pod.Containers {
			initial, exists := original.Containers[container]
			// RestartCount is best effort and can reset after kubelet loses state.
			newOOM := status.LastTermination != nil && status.LastTermination.Reason == "OOMKilled" &&
				!reflect.DeepEqual(initial.LastTermination, status.LastTermination)
			if !exists || initial.RestartCount != status.RestartCount || newOOM {
				return fmt.Errorf("verify login pod %s container %s: before=%+v after=%+v", name, container, initial, status)
			}
		}
	}
	return nil
}

func (s *LoginUserIsolation) containerCgroup() string {
	return path.Dir(path.Dir(s.sessions[0].Memory.Cgroup))
}

func (s *LoginUserIsolation) mountedMemoryConfig(ctx context.Context, pod string) (string, error) {
	output, err := s.runtime.Kubectl().Run(ctx, "exec", "-n", framework.SoperatorNamespace, pod,
		"-c", loginSSHDContainer, "--", "cat", "/etc/soperator/user-isolation.conf")
	if err != nil {
		return "", fmt.Errorf("read mounted user isolation configuration in login pod %s: %w", pod, err)
	}
	return output, nil
}

func (s *LoginUserIsolation) waitRestoredMemory(ctx context.Context) error {
	_, err := s.waitMemoryRollout(ctx, "", false, func(waitCtx context.Context, pod corev1.Pod) error {
		config, err := s.mountedMemoryConfig(waitCtx, pod.Name)
		if err != nil {
			return err
		}
		if config != s.originalConfig {
			return fmt.Errorf("wait for restored memory configuration in login pod %s: got %q, expected %q", pod.Name, config, s.originalConfig)
		}
		return nil
	})
	return err
}

func (s *LoginUserIsolation) probeCommand(action string, args ...string) string {
	timeout := loginMemoryHighTimeout
	if action == "hard" {
		timeout = loginMemoryHardTimeout
	}
	command := fmt.Sprintf("python3 %s --memory-high %d --memory-max %d --held-bytes %d --hold-seconds %.0f --workload-timeout %.0f %s",
		framework.ShellQuote(path.Join(s.workdir, "probe.py")), loginMemoryHigh, loginMemoryMax, loginMemoryHeld, loginMemoryHold.Seconds(), timeout.Seconds(), framework.ShellQuote(action))
	for _, arg := range args {
		command += " " + framework.ShellQuote(arg)
	}
	return command
}

func (s *LoginUserIsolation) sshProbe(ctx context.Context, user, action, reportKey string, target any) error {
	output, err := runSSHCommand(ctx, s.runtime, user, "id_ecdsa", "localhost", loginMemoryCommand, s.probeCommand(action))
	if err != nil {
		return fmt.Errorf("run login memory %s probe as %s: %w; output: %s", action, user, err, output)
	}
	return s.decodeProbe(reportKey, output, target)
}

func (s *LoginUserIsolation) rootProbe(ctx context.Context, action, reportKey string, target any, args ...string) error {
	output, err := s.runtime.Jail().Run(ctx, s.probeCommand(action, args...))
	if err != nil {
		return fmt.Errorf("run login memory %s root probe: %w", action, err)
	}
	return s.decodeProbe(reportKey, output, target)
}

func (s *LoginUserIsolation) decodeProbe(name, output string, target any) error {
	raw := parseKeyValueLine(output, "LOGIN_MEMORY")
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("decode login memory %s probe: %w; output: %s", name, err, output)
	}
	s.record(name, json.RawMessage(raw))
	return nil
}

func (s *LoginUserIsolation) record(key string, value any) {
	s.report[key] = value
}

func (s *LoginUserIsolation) writeReport() {
	if s.report == nil || s.artifactDir == "" {
		return
	}
	data, err := json.MarshalIndent(s.report, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(s.artifactDir, "login-memory-isolation.json"), data, 0o600)
	}
	if err != nil {
		s.runtime.Logf("Write login memory isolation artifact: %v", err)
	}
}

func (s *LoginUserIsolation) restoreMemoryAutoscaling(ctx context.Context) error {
	original := s.originalAutoscaling
	s.frozenReplicas = 0
	var config any
	if err := json.Unmarshal(original.Configuration, &config); err != nil {
		return fmt.Errorf("decode original login autoscaling: %w", err)
	}
	if err := s.patchLogin(ctx, map[string]any{"size": original.Size, "autoscaling": config}); err != nil {
		return err
	}
	if err := s.waitLoginHPA(ctx, original.StatefulSetName, true, loginAutoscalingCleanupTimeout); err != nil {
		return err
	}
	_, err := s.waitMemoryRollout(ctx, "", false, nil)
	return err
}

func (s *LoginUserIsolation) CleanupAndReset(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if s.workdir != "" {
		for _, user := range []string{loginMemoryFirstUser, loginMemorySecondUser} {
			command := "pkill -u " + framework.ShellQuote(user) + " -f " + framework.ShellQuote(s.workdir) + " 2>/dev/null || true"
			if _, err := s.runtime.Jail().Run(cleanupCtx, command); err != nil {
				s.runtime.Logf("Cleanup login memory processes for %s: %v", user, err)
			}
		}
		if _, err := s.runtime.Jail().Run(cleanupCtx, "rm -rf -- "+framework.ShellQuote(s.workdir)); err != nil {
			s.runtime.Logf("Cleanup login memory directory %s: %v", s.workdir, err)
		}
	}
	if s.originalLimits != nil {
		restoreCtx, restoreCancel := context.WithTimeout(ctx, loginMemoryRollout)
		defer restoreCancel()
		err := s.patchLogin(restoreCtx, map[string]any{"userIsolation": s.originalLimits})
		if err == nil {
			err = s.waitRestoredMemory(restoreCtx)
		}
		result := "restored"
		if err != nil {
			result = err.Error()
			s.runtime.Logf("Cleanup restore login memory limits: %v", err)
		}
		s.record("cleanup", result)
	}
	if s.originalAutoscaling != nil {
		restoreCtx, restoreCancel := context.WithTimeout(ctx, loginAutoscalingCleanupTimeout)
		defer restoreCancel()
		result := "restored"
		if err := s.restoreMemoryAutoscaling(restoreCtx); err != nil {
			result = err.Error()
			s.runtime.Logf("Cleanup restore login autoscaling: %v", err)
		}
		s.record("autoscaling_cleanup", result)
	}
	s.writeReport()
	workload := s.loginWorkload
	*s = LoginUserIsolation{loginWorkload: workload}
}
