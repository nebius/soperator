package sharedsteps

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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

type memoryLifecycleRuntime struct {
	framework.Runtime
	cluster            kubeobjects.SlurmCluster
	replicas           int32
	hpa                bool
	config             string
	stage              string
	failStage          string
	environment        loginMemoryEnvironment
	patches            []map[string]any
	stsReads, podReads int
	updateStrategy     string
	podRevisions       map[string]string
	restarts           []string
	restartFailStage   string
}

func newMemoryLifecycleRuntime(t *testing.T) *memoryLifecycleRuntime {
	t.Helper()
	r := &memoryLifecycleRuntime{replicas: 2, hpa: true, config: "original"}
	err := json.Unmarshal([]byte(`{"spec":{"slurmNodes":{"login":{"size":1,"userIsolation":{"enabled":true},"autoscaling":{"enabled":true,"minReplicas":1,"maxReplicas":3},"sshd":{"resources":{"memory":"4Gi"}}}}}}`), &r.cluster)
	require.NoError(t, err)
	return r
}

func (r *memoryLifecycleRuntime) Logf(string, ...any) {}

func (r *memoryLifecycleRuntime) Jail() framework.CommandScope {
	return framework.NewCommandScope(func(_ context.Context, command string) (string, error) {
		if strings.Contains(command, "'environment'") {
			data, err := json.Marshal(r.environment)
			return "LOGIN_MEMORY=" + string(data), err
		}
		return "", nil
	})
}

func (r *memoryLifecycleRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		var value any
		switch args[0] {
		case "exec":
			if args[len(args)-2] == "soperator-login-restart" {
				if strings.Join(args[4:], " ") != "-c "+loginSSHDContainer+" -- chroot /mnt/jail soperator-login-restart --yes" {
					return "", fmt.Errorf("unexpected restart command: %v", args)
				}
				if r.restartFailStage != "" && r.stage == r.restartFailStage {
					return "", fmt.Errorf("restart request rejected")
				}
				name := args[3]
				r.restarts = append(r.restarts, r.stage+":"+name)
				r.podRevisions[name] = r.desiredRevision()
				return "Restart requested", nil
			}
			return r.config, nil
		case "patch":
			var patch struct {
				Spec struct {
					SlurmNodes struct {
						Login map[string]any `json:"login"`
					} `json:"slurmNodes"`
				} `json:"spec"`
			}
			if err := json.Unmarshal([]byte(args[len(args)-1]), &patch); err != nil {
				return "", err
			}
			login := patch.Spec.SlurmNodes.Login
			r.patches = append(r.patches, login)
			if size, ok := login["size"].(float64); ok {
				r.replicas = int32(size)
			}
			if scaling, ok := login["autoscaling"].(map[string]any); ok {
				r.hpa, _ = scaling["enabled"].(bool)
				r.stage = "freeze"
				if r.hpa {
					r.stage = "autoscaling_restored"
				}
			}
			if limits, ok := login["userIsolation"].(map[string]any); ok {
				r.stage, r.config = "memory", "test limits"
				var original map[string]any
				if err := json.Unmarshal(r.cluster.Spec.SlurmNodes.Login.UserIsolation, &original); err != nil {
					return "", err
				}
				if limits["memoryHigh"] == original["memoryHigh"] && limits["memoryMax"] == original["memoryMax"] {
					r.stage, r.config = "restored", "original"
				}
			}
			return "patched", nil
		case "get":
			switch args[1] {
			case "slurmcluster":
				value = r.cluster
			case loginHPAResource:
				if !r.hpa {
					return "", nil
				}
				return `{"metadata":{"name":"login"}}`, nil
			case kruiseStatefulSetResource:
				r.stsReads++
				sts := memoryTestStatefulSet()
				sts.Metadata.Name = "login"
				if r.config == "test limits" {
					sts.Status.CurrentRevision, sts.Status.UpdateRevision = "configured", "configured"
				}
				sts.Spec.Replicas = &r.replicas
				sts.Spec.UpdateStrategy.Type = r.updateStrategy
				sts.Status.ReadyReplicas, sts.Status.UpdatedReadyReplicas = r.replicas, r.replicas
				if r.updateStrategy == "OnDelete" {
					sts.Status.CurrentRevision, sts.Status.UpdateRevision = "old", r.desiredRevision()
					sts.Status.UpdatedReadyReplicas = 0
					for i := int32(0); i < r.replicas; i++ {
						if r.podRevisions["login-"+strconv.Itoa(int(i))] == sts.Status.UpdateRevision {
							sts.Status.UpdatedReadyReplicas++
						}
					}
				}
				if r.failStage != "" && r.stage == r.failStage {
					sts.Status.UpdatedReadyReplicas = 0
				}
				value = kubeobjects.LoginStatefulSetList{Items: []kubeobjects.LoginStatefulSet{sts}}
			case "pods":
				r.podReads++
				var pods []corev1.Pod
				for i := int32(0); i < r.replicas; i++ {
					pod := memoryTestPod()
					pod.Name = "login-" + strconv.Itoa(int(i))
					pod.UID = types.UID(pod.Name)
					if r.config == "test limits" {
						pod.Labels["controller-revision-hash"] = "configured"
					}
					if r.updateStrategy == "OnDelete" {
						pod.Labels["controller-revision-hash"] = r.podRevisions[pod.Name]
						pod.UID = types.UID(pod.Name + "-" + r.podRevisions[pod.Name])
						if r.podRevisions[pod.Name] != r.desiredRevision() {
							pod.Annotations = map[string]string{loginUpdateAnnounced: "2026-11-12T00:00:00Z", loginUpdateScheduled: "2026-12-12T00:00:00Z"}
						}
					}
					pods = append(pods, pod)
				}
				value = corev1.PodList{Items: pods}
			default:
				return "", fmt.Errorf("unexpected get: %v", args)
			}
		default:
			return "", fmt.Errorf("unexpected command: %v", args)
		}
		data, err := json.Marshal(value)
		return string(data), err
	})
}

func (r *memoryLifecycleRuntime) desiredRevision() string {
	if r.config == "test limits" {
		return "configured"
	}
	return "new"
}

func (r *memoryLifecycleRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	var err error
	for range 8 {
		var done bool
		done, err = condition(ctx)
		if done && err == nil {
			return nil
		}
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("condition is pending")
}

func TestMemoryScenarioRestartsDelayedPodsDuringSetupAndRestoration(t *testing.T) {
	runtime := newMemoryLifecycleRuntime(t)
	runtime.updateStrategy = "OnDelete"
	runtime.podRevisions = map[string]string{"login-0": "old", "login-1": "old"}
	s := NewLoginUserIsolation(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, framework.NewKubectlClient(runtime))
	require.NoError(t, s.configureMemory(t.Context()))
	assert.Equal(t, []string{":login-0", ":login-1", "memory:login-0", "memory:login-1"}, runtime.restarts)
	for _, pod := range s.pods {
		assert.Contains(t, pod.UID, "configured")
	}
	report := s.report
	s.CleanupAndReset(t.Context())
	assert.Equal(t, []string{":login-0", ":login-1", "memory:login-0", "memory:login-1", "restored:login-0", "restored:login-1"}, runtime.restarts)
	assert.Equal(t, "restored", report["cleanup"])
	assert.Equal(t, "restored", report["autoscaling_cleanup"])
	assert.True(t, runtime.hpa)
	assert.Equal(t, "OnDelete", runtime.updateStrategy)
}

func TestMemoryScenarioRestoresAfterDelayedRestartRequestFails(t *testing.T) {
	runtime := newMemoryLifecycleRuntime(t)
	runtime.updateStrategy, runtime.restartFailStage = "OnDelete", "memory"
	runtime.podRevisions = map[string]string{"login-0": "new", "login-1": "new"}
	s := NewLoginUserIsolation(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, framework.NewKubectlClient(runtime))
	require.ErrorContains(t, s.configureMemory(t.Context()), "restart request rejected")
	report := s.report
	s.CleanupAndReset(t.Context())
	assert.Equal(t, "original", runtime.config)
	assert.Equal(t, "restored", report["cleanup"])
	assert.Equal(t, "restored", report["autoscaling_cleanup"])
	assert.True(t, runtime.hpa)
}

func TestMemoryScenarioRequestsOnlyAnnouncedOutdatedPods(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*kubeobjects.LoginStatefulSet, *corev1.Pod)
		restart bool
	}{
		{name: "announced", restart: true},
		{name: "rolling update", mutate: func(sts *kubeobjects.LoginStatefulSet, _ *corev1.Pod) { sts.Spec.UpdateStrategy.Type = "RollingUpdate" }},
		{name: "legacy strategy absent", mutate: func(sts *kubeobjects.LoginStatefulSet, _ *corev1.Pod) { sts.Spec.UpdateStrategy.Type = "" }},
		{name: "current", mutate: func(_ *kubeobjects.LoginStatefulSet, pod *corev1.Pod) { pod.Labels["controller-revision-hash"] = "new" }},
		{name: "terminating", mutate: func(_ *kubeobjects.LoginStatefulSet, pod *corev1.Pod) {
			pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		}},
		{name: "unready", mutate: func(_ *kubeobjects.LoginStatefulSet, pod *corev1.Pod) { pod.Status.Conditions = nil }},
		{name: "unannounced", mutate: func(_ *kubeobjects.LoginStatefulSet, pod *corev1.Pod) { delete(pod.Annotations, loginUpdateAnnounced) }},
		{name: "unscheduled", mutate: func(_ *kubeobjects.LoginStatefulSet, pod *corev1.Pod) { delete(pod.Annotations, loginUpdateScheduled) }},
		{name: "already requested", mutate: func(_ *kubeobjects.LoginStatefulSet, pod *corev1.Pod) {
			pod.Annotations[loginUpdateRequested] = string(pod.UID)
		}},
		{name: "request for previous pod instance", mutate: func(_ *kubeobjects.LoginStatefulSet, pod *corev1.Pod) {
			pod.Annotations[loginUpdateRequested] = "previous-uid"
		}, restart: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := newMemoryLifecycleRuntime(t)
			runtime.podRevisions = make(map[string]string)
			sts, pod := memoryTestStatefulSet(), memoryTestPod()
			sts.Spec.UpdateStrategy.Type = "OnDelete"
			pod.Labels["controller-revision-hash"] = "old"
			pod.Annotations = map[string]string{loginUpdateAnnounced: "announced", loginUpdateScheduled: "scheduled"}
			if test.mutate != nil {
				test.mutate(&sts, &pod)
			}
			s := NewLoginUserIsolation(&framework.ClusterInfo{}, runtime, framework.NewKubectlClient(runtime))
			require.NoError(t, s.requestMemoryPodUpdates(t.Context(), sts, []corev1.Pod{pod}))
			assert.Equal(t, test.restart, len(runtime.restarts) == 1)
		})
	}
}

func TestMemoryScenarioFreezesAndRestoresAutoscaling(t *testing.T) {
	runtime := newMemoryLifecycleRuntime(t)
	// The memory limits already match, so a revision change is not required by this fixture.
	runtime.cluster.Spec.SlurmNodes.Login.UserIsolation = json.RawMessage(`{"enabled":true,"memoryHigh":"480Mi","memoryMax":"512Mi"}`)
	s := NewLoginUserIsolation(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, framework.NewKubectlClient(runtime))
	require.NoError(t, s.configureMemory(t.Context()))
	require.Len(t, runtime.patches, 2)
	assert.Equal(t, int32(2), runtime.replicas)
	assert.False(t, runtime.hpa)
	assert.Equal(t, float64(2), runtime.patches[0]["size"])
	assert.Len(t, s.pods, 2)
	assert.Equal(t, 3, runtime.stsReads, "reuse each verified rollout snapshot")
	assert.Equal(t, 3, runtime.podReads)
	report := s.report
	s.CleanupAndReset(t.Context())
	assert.True(t, runtime.hpa)
	assert.Equal(t, int32(1), runtime.replicas)
	assert.Equal(t, "restored", report["autoscaling_cleanup"])
	assert.Equal(t, "restored", report["cleanup"])
	restored := runtime.patches[len(runtime.patches)-1]["autoscaling"]
	data, err := json.Marshal(restored)
	require.NoError(t, err)
	assert.JSONEq(t, `{"enabled":true,"minReplicas":1,"maxReplicas":3}`, string(data))
}

func TestMemoryScenarioRestoresHPAAfterPartialSetup(t *testing.T) {
	for _, stage := range []string{"freeze", "memory"} {
		t.Run(stage, func(t *testing.T) {
			runtime := newMemoryLifecycleRuntime(t)
			runtime.failStage = stage
			s := NewLoginUserIsolation(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, framework.NewKubectlClient(runtime))
			require.Error(t, s.configureMemory(t.Context()))
			assert.False(t, runtime.hpa)
			report := s.report
			s.CleanupAndReset(t.Context())
			assert.True(t, runtime.hpa)
			assert.Equal(t, int32(1), runtime.replicas)
			assert.Equal(t, "original", runtime.config)
			assert.Equal(t, "restored", report["autoscaling_cleanup"])
		})
	}
}

func TestMemoryScenarioSkipsUsableSwapBeforeCRPatch(t *testing.T) {
	runtime := newMemoryLifecycleRuntime(t)
	runtime.environment = loginMemoryEnvironment{Swap: true, SwapLimits: map[string]string{"/container": "max"}}
	s := NewLoginUserIsolation(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, framework.NewKubectlClient(runtime))
	require.ErrorIs(t, s.configureMemory(t.Context()), godog.ErrSkip)
	assert.Empty(t, runtime.patches)
	s.CleanupAndReset(t.Context())
	assert.Empty(t, runtime.patches)
	assert.True(t, runtime.hpa)
}

func TestMemoryScenarioAcceptsInheritedSwapBlockWithoutHPA(t *testing.T) {
	runtime := newMemoryLifecycleRuntime(t)
	runtime.hpa, runtime.replicas = false, 1
	runtime.cluster.Spec.SlurmNodes.Login.Autoscaling = json.RawMessage(`{"enabled":false}`)
	runtime.environment = loginMemoryEnvironment{Swap: true, SwapDisabled: true,
		SwapLimits: map[string]string{"/container": "0", "/container/users/user-1000": "max"}}
	s := NewLoginUserIsolation(&framework.ClusterInfo{SlurmClusterName: "soperator"}, runtime, framework.NewKubectlClient(runtime))
	require.NoError(t, s.configureMemory(t.Context()))
	require.Len(t, runtime.patches, 1)
	assert.Nil(t, s.originalAutoscaling)
	report := s.report
	s.CleanupAndReset(t.Context())
	assert.False(t, runtime.hpa)
	assert.Equal(t, int32(1), runtime.replicas)
	assert.Equal(t, "restored", report["cleanup"])
}
