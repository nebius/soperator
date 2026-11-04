package framework

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type replacementRuntime struct {
	Runtime
	instanceLookups []error
	nodeStates      []string
	kubectlOutputs  map[string]string
	kubectlErrors   map[string]error
}

func (r *replacementRuntime) Kubectl() ArgsScope {
	return NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		key := strings.Join(args, "\x00")
		if err := r.kubectlErrors[key]; err != nil {
			return "", err
		}
		out, ok := r.kubectlOutputs[key]
		if !ok {
			return "", fmt.Errorf("unexpected kubectl command: %v", args)
		}
		return out, nil
	})
}

func (r *replacementRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, condition func(context.Context) (bool, error)) error {
	for range 4 {
		ready, err := condition(ctx)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
	}
	return errors.New("timed out")
}

func (r *replacementRuntime) Local() ArgsScope {
	return NewArgsScope(func(_ context.Context, args ...string) (string, error) {
		if len(args) != 8 || args[0] != "nebius" || args[4] != "--id" {
			return "", fmt.Errorf("unexpected command: %v", args)
		}
		err := r.instanceLookups[0]
		r.instanceLookups = r.instanceLookups[1:]
		return "", err
	})
}

func (r *replacementRuntime) Controller() CommandScope {
	return NewCommandScope(func(_ context.Context, command string) (string, error) {
		if command != "scontrol show node 'worker-0'" {
			return "", fmt.Errorf("unexpected command: %s", command)
		}
		state := r.nodeStates[0]
		r.nodeStates = r.nodeStates[1:]
		return state, nil
	})
}

func TestWaitForInstanceRemoval(t *testing.T) {
	runtime := &replacementRuntime{instanceLookups: []error{nil, errors.New("instance not found")}}
	require.NoError(t, WaitForInstanceRemoval(t.Context(), runtime, "old-instance", time.Minute, time.Second))
}

func TestWaitForInstanceRemovalPropagatesLookupError(t *testing.T) {
	runtime := &replacementRuntime{instanceLookups: []error{errors.New("permission denied")}}
	err := WaitForInstanceRemoval(t.Context(), runtime, "old-instance", time.Minute, time.Second)
	require.ErrorContains(t, err, "permission denied")
}

func TestWaitForUsableReplacement(t *testing.T) {
	runtime := &replacementRuntime{nodeStates: []string{
		"NodeName=worker-0 InstanceId=old-instance State=IDLE",
		"NodeName=worker-0 InstanceId=new-instance State=IDLE+DRAIN",
		"NodeName=worker-0 InstanceId=new-instance",
		"NodeName=worker-0 InstanceId=new-instance State=IDLE",
	}}
	err := WaitForUsableReplacement(t.Context(), runtime, NewSlurmClient(runtime), "worker-0", "old-instance", time.Minute, time.Second)
	require.NoError(t, err)
	require.Empty(t, runtime.nodeStates)
}

func TestCaptureWorkerReplacementRecordsActualPodAndHostIdentities(t *testing.T) {
	runtime, kubectl, slurm, original := workerReplacementFixture(t)
	pod := replacementWorkerPod()
	pod.Name, pod.UID, pod.Spec.NodeName = original.PodName, original.PodUID, original.NodeName
	runtime.kubectlOutputs["get\x00pods\x00-n\x00soperator\x00-l\x00slurm.nebius.ai/worker=true\x00-o\x00json"] = replacementJSON(t, corev1.PodList{Items: []corev1.Pod{pod}})
	runtime.kubectlOutputs["get\x00pod\x00worker-pod-0\x00-n\x00soperator\x00-o\x00json"] = replacementJSON(t, pod)
	runtime.kubectlOutputs["get\x00node\x00original-host\x00--ignore-not-found\x00-o\x00json"] = replacementJSON(t, corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: original.NodeName, UID: original.NodeUID},
	})
	runtime.nodeStates = []string{"NodeName=worker-0 InstanceId=original-instance State=IDLE"}
	captured, err := CaptureWorkerReplacement(t.Context(), kubectl, slurm, original.WorkerName)
	require.NoError(t, err)
	require.Equal(t, original, captured)
	// Kubernetes host names do not have to equal Compute instance IDs.
	require.NotEqual(t, captured.NodeName, captured.InstanceID)
}

func TestCaptureWorkerReplacementRejectsIncompleteOrUnreadyBaseline(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*corev1.Pod, *replacementRuntime)
		want   string
	}{
		{"missing Pod UID", func(pod *corev1.Pod, _ *replacementRuntime) { pod.UID = "" }, "record Ready worker"},
		{"not Ready", func(pod *corev1.Pod, _ *replacementRuntime) { pod.Status.Conditions = nil }, "record Ready worker"},
		{"deleting Pod", func(pod *corev1.Pod, _ *replacementRuntime) { pod.DeletionTimestamp = new(metav1.Now()) }, "record Ready worker"},
		{"wrong hostname", func(pod *corev1.Pod, _ *replacementRuntime) { pod.Spec.Hostname = "other-worker" }, "record Ready worker"},
		{"host removed during capture", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.kubectlOutputs["get\x00node\x00original-host\x00--ignore-not-found\x00-o\x00json"] = ""
		}, "record worker worker-0 host"},
		{"missing Compute identity", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.nodeStates = []string{"NodeName=worker-0 State=IDLE"}
		}, "record Compute instance"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime, kubectl, slurm, original := workerReplacementFixture(t)
			pod := replacementWorkerPod()
			pod.Name, pod.UID, pod.Spec.NodeName = original.PodName, original.PodUID, original.NodeName
			runtime.kubectlOutputs["get\x00pods\x00-n\x00soperator\x00-l\x00slurm.nebius.ai/worker=true\x00-o\x00json"] = replacementJSON(t, corev1.PodList{Items: []corev1.Pod{pod}})
			runtime.kubectlOutputs["get\x00node\x00original-host\x00--ignore-not-found\x00-o\x00json"] = replacementJSON(t, corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: original.NodeName, UID: original.NodeUID},
			})
			runtime.nodeStates = []string{"NodeName=worker-0 InstanceId=original-instance State=IDLE"}
			tt.change(&pod, runtime)
			runtime.kubectlOutputs["get\x00pod\x00worker-pod-0\x00-n\x00soperator\x00-o\x00json"] = replacementJSON(t, pod)
			_, err := CaptureWorkerReplacement(t.Context(), kubectl, slurm, original.WorkerName)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestObserveWorkerReplacementRequiresEveryOriginalIdentityReplaced(t *testing.T) {
	for _, tt := range []struct {
		name      string
		change    func(*corev1.Pod, *replacementRuntime)
		wantReady bool
		wantError string
	}{
		{"all identities replaced", func(_ *corev1.Pod, _ *replacementRuntime) {}, true, ""},
		{"original Pod still present", func(pod *corev1.Pod, _ *replacementRuntime) { pod.UID = "original-pod-uid" }, false, ""},
		{"new Pod on original host", func(pod *corev1.Pod, _ *replacementRuntime) { pod.Spec.NodeName = "original-host" }, false, ""},
		{"new Pod not Ready", func(pod *corev1.Pod, _ *replacementRuntime) { pod.Status.Conditions = nil }, false, ""},
		{"new Pod deleting", func(pod *corev1.Pod, _ *replacementRuntime) { pod.DeletionTimestamp = new(metav1.Now()) }, false, ""},
		{"wrong stable worker name", func(pod *corev1.Pod, _ *replacementRuntime) { pod.Spec.Hostname = "other-worker" }, false, "identity changed"},
		{"original host still present", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.kubectlOutputs["get\x00node\x00original-host\x00--ignore-not-found\x00-o\x00json"] = replacementJSON(t, corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "original-host", UID: "original-node-uid"},
			})
		}, false, ""},
		{"original host name reused with new UID", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.kubectlOutputs["get\x00node\x00original-host\x00--ignore-not-found\x00-o\x00json"] = replacementJSON(t, corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "original-host", UID: "new-node-uid"},
			})
		}, true, ""},
		{"original Compute registration", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.nodeStates = []string{"NodeName=worker-0 InstanceId=original-instance State=IDLE"}
		}, false, ""},
		{"new Slurm registration remains drained", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.nodeStates = []string{"NodeName=worker-0 InstanceId=new-instance State=IDLE+DRAIN"}
		}, false, ""},
		{"missing new Compute registration", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.nodeStates = []string{"NodeName=worker-0 State=IDLE"}
		}, false, ""},
		{"old Compute instance exists", func(_ *corev1.Pod, runtime *replacementRuntime) { runtime.instanceLookups = []error{nil} }, false, ""},
		{"Compute authorization fails", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.instanceLookups = []error{errors.New("permission denied")}
		}, false, "permission denied"},
		{"Kubernetes authorization fails", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.kubectlErrors = map[string]error{"get\x00node\x00original-host\x00--ignore-not-found\x00-o\x00json": errors.New("forbidden")}
		}, false, "forbidden"},
		{"host response identity missing", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.kubectlOutputs["get\x00node\x00original-host\x00--ignore-not-found\x00-o\x00json"] = `{}`
		}, false, "identify Kubernetes host"},
		{"host response UID missing", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.kubectlOutputs["get\x00node\x00original-host\x00--ignore-not-found\x00-o\x00json"] = `{"metadata":{"name":"original-host"}}`
		}, false, "identify Kubernetes host"},
		{"host response name mismatch", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.kubectlOutputs["get\x00node\x00original-host\x00--ignore-not-found\x00-o\x00json"] = `{"metadata":{"name":"other-host","uid":"new-node-uid"}}`
		}, false, "identify Kubernetes host"},
		{"host response malformed", func(_ *corev1.Pod, runtime *replacementRuntime) {
			runtime.kubectlOutputs["get\x00node\x00original-host\x00--ignore-not-found\x00-o\x00json"] = `{`
		}, false, "decode Kubernetes host"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime, kubectl, slurm, original := workerReplacementFixture(t)
			pod := replacementWorkerPod()
			tt.change(&pod, runtime)
			runtime.kubectlOutputs["get\x00pod\x00worker-pod-0\x00-n\x00soperator\x00-o\x00json"] = replacementJSON(t, pod)
			observed, ready, err := ObserveWorkerReplacement(t.Context(), runtime, kubectl, slurm, original)
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
			require.Equal(t, tt.wantReady, ready)
			require.JSONEq(t, replacementJSON(t, pod), replacementJSON(t, observed))
		})
	}
}

func TestObserveWorkerReplacementRejectsIncompleteSnapshot(t *testing.T) {
	runtime, kubectl, slurm, original := workerReplacementFixture(t)
	original.NodeUID = ""
	_, ready, err := ObserveWorkerReplacement(t.Context(), runtime, kubectl, slurm, original)
	require.ErrorContains(t, err, "original identities are incomplete")
	require.False(t, ready)
}

func workerReplacementFixture(t *testing.T) (*replacementRuntime, *KubectlClient, *SlurmClient, WorkerReplacementSnapshot) {
	t.Helper()
	original := WorkerReplacementSnapshot{
		Namespace: "soperator", WorkerName: "worker-0", PodName: "worker-pod-0", PodUID: "original-pod-uid",
		NodeName: "original-host", NodeUID: "original-node-uid", InstanceID: "original-instance",
	}
	runtime := &replacementRuntime{
		kubectlOutputs: map[string]string{
			"get\x00pod\x00worker-pod-0\x00-n\x00soperator\x00-o\x00json":      replacementJSON(t, replacementWorkerPod()),
			"get\x00node\x00original-host\x00--ignore-not-found\x00-o\x00json": "",
		},
		nodeStates:      []string{"NodeName=worker-0 InstanceId=new-instance State=IDLE"},
		instanceLookups: []error{errors.New("instance not found")},
	}
	return runtime, NewKubectlClient(runtime), NewSlurmClient(runtime), original
}

func replacementWorkerPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-pod-0", Namespace: "soperator", UID: "new-pod-uid"},
		Spec:       corev1.PodSpec{Hostname: "worker-0", NodeName: "new-host"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{
			{Type: corev1.PodReady, Status: corev1.ConditionTrue},
		}},
	}
}

func replacementJSON(t *testing.T, object any) string {
	t.Helper()
	encoded, err := json.Marshal(object)
	require.NoError(t, err)
	return string(encoded)
}
