package sharedsteps

import (
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

func readyWorkerPod(name string, uid types.UID, annotations map[string]string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: uid, Annotations: annotations},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
}

func terminatingPod(deletedAt time.Time, graceSeconds int64) corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		DeletionTimestamp:          new(metav1.NewTime(deletedAt)),
		DeletionGracePeriodSeconds: new(graceSeconds),
	}}
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
	assert.Empty(t, workerRolloutCandidateProblem(ready, "soperator"))

	tests := []struct {
		name   string
		mutate func(*kubeobjects.NodeSet)
		want   string
	}{
		{"other cluster", func(n *kubeobjects.NodeSet) { n.Spec.ClusterName = "other" }, "belongs to another cluster"},
		{"cpu", func(n *kubeobjects.NodeSet) { n.Spec.GPU.Enabled = false }, "no GPU"},
		{"kruise rolling update", func(n *kubeobjects.NodeSet) { n.Spec.UpdateStrategy = "rollingUpdate" }, `updateStrategy="rollingUpdate"`},
		{"ephemeral", func(n *kubeobjects.NodeSet) { n.Spec.EphemeralNodes = new(true) }, "ephemeral mode"},
		{"no replicas", func(n *kubeobjects.NodeSet) { n.Spec.Replicas = 0 }, "no replicas"},
		{"not ready", func(n *kubeobjects.NodeSet) { n.Status.Replicas = 1 }, "phase=Ready ready=1/2"},
		{"provisioning", func(n *kubeobjects.NodeSet) { n.Status.Phase = "Provisioning" }, "phase=Provisioning ready=2/2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeSet := ready
			tt.mutate(&nodeSet)
			assert.Equal(t, tt.want, workerRolloutCandidateProblem(nodeSet, "soperator"))
		})
	}
}

func TestPodTerminatingPastDeadline(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	past, _ := podTerminatingPastDeadline(corev1.Pod{}, now)
	assert.False(t, past, "a pod without deletionTimestamp is not terminating")

	past, terminatingFor := podTerminatingPastDeadline(terminatingPod(now.Add(-100*time.Second), 30), now)
	assert.False(t, past, "30s grace plus the 90s margin is not over yet")
	assert.Equal(t, 100*time.Second, terminatingFor)

	past, terminatingFor = podTerminatingPastDeadline(terminatingPod(now.Add(-200*time.Second), 30), now)
	assert.True(t, past)
	assert.Equal(t, 200*time.Second, terminatingFor)

	past, _ = podTerminatingPastDeadline(terminatingPod(now.Add(-200*time.Second), 300), now)
	assert.False(t, past, "a longer grace period extends the deadline")
}

func TestReplacedWorkerPods(t *testing.T) {
	initial := map[string]workerPodSnapshot{
		"worker-0": {UID: "old-0"},
		"worker-1": {UID: "old-1"},
	}
	newTemplate := map[string]string{workerRolloutAnnotationKey: "v2"}

	t.Run("all replaced", func(t *testing.T) {
		pods := []corev1.Pod{readyWorkerPod("worker-0", "new-0", newTemplate), readyWorkerPod("worker-1", "new-1", newTemplate)}
		assert.Empty(t, replacedWorkerPods("worker", initial, pods, 2, workerRolloutAnnotationKey, "v2"))
	})

	t.Run("mixed progress", func(t *testing.T) {
		terminating := readyWorkerPod("worker-1", "new-1", newTemplate)
		terminating.DeletionTimestamp = new(metav1.Now())
		notReady := readyWorkerPod("worker-2", "new-2", newTemplate)
		notReady.Status.Phase = corev1.PodPending
		notReady.Status.Conditions = nil
		pods := []corev1.Pod{
			readyWorkerPod("worker-0", "old-0", nil),
			terminating,
			notReady,
			readyWorkerPod("worker-3", "new-3", map[string]string{workerRolloutAnnotationKey: "v1"}),
		}
		remaining := replacedWorkerPods("worker", initial, pods, 5, workerRolloutAnnotationKey, "v2")
		assert.Equal(t, []string{
			"worker-0 (old pod)",
			"worker-1 (terminating)",
			"worker-2 (Pending, not ready)",
			"worker-3 (old template)",
			"worker-4 (missing)",
		}, remaining)
	})
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

func TestDaemonSetPodStartedAfter(t *testing.T) {
	workerStart := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	daemonSetPod := func(start time.Time, phase corev1.PodPhase) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet"}}},
			Status:     corev1.PodStatus{Phase: phase, StartTime: new(metav1.NewTime(start))},
		}
	}

	assert.True(t, daemonSetPodStartedAfter(workerStart, []corev1.Pod{daemonSetPod(workerStart.Add(time.Minute), corev1.PodRunning)}))
	assert.False(t, daemonSetPodStartedAfter(workerStart, []corev1.Pod{daemonSetPod(workerStart.Add(-time.Minute), corev1.PodRunning)}))
	assert.False(t, daemonSetPodStartedAfter(workerStart, []corev1.Pod{daemonSetPod(workerStart.Add(time.Minute), corev1.PodPending)}))
	assert.False(t, daemonSetPodStartedAfter(workerStart, nil))

	deployment := daemonSetPod(workerStart.Add(time.Minute), corev1.PodRunning)
	deployment.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet"}}
	assert.False(t, daemonSetPodStartedAfter(workerStart, []corev1.Pod{deployment}))

	noStart := daemonSetPod(workerStart, corev1.PodRunning)
	noStart.Status.StartTime = nil
	assert.False(t, daemonSetPodStartedAfter(workerStart, []corev1.Pod{noStart}))
}

func TestIdleProblems(t *testing.T) {
	assert.Empty(t, idleProblems([]framework.SlurmNodeInfo{
		{Name: "worker-0", State: "IDLE"},
		{Name: "worker-1", State: "IDLE+CLOUD"},
	}))

	problems := idleProblems([]framework.SlurmNodeInfo{
		{Name: "worker-0", State: "IDLE+DRAIN", Reason: "soperator rolling update"},
		{Name: "worker-1", State: "MIXED"},
		{Name: "worker-2", State: "IDLE", Reason: "stale"},
		{Name: "worker-3", State: "IDLE"},
	})
	require.Len(t, problems, 3)
	assert.Contains(t, problems[0], "worker-0 state=IDLE+DRAIN")
	assert.Contains(t, problems[0], "soperator rolling update")
	assert.Contains(t, problems[1], "worker-1 state=MIXED")
	assert.Contains(t, problems[2], "worker-2 state=IDLE")
}

func TestJobOutcome(t *testing.T) {
	tests := []struct {
		name        string
		info        framework.SlurmJobInfo
		wantDone    bool
		wantFailure string
	}{
		{"running", framework.SlurmJobInfo{QueueState: "RUNNING"}, false, ""},
		{"completing", framework.SlurmJobInfo{QueueState: "COMPLETING"}, false, ""},
		{"gone without accounting record yet", framework.SlurmJobInfo{}, false, ""},
		{"completed", framework.SlurmJobInfo{ID: "7", SacctFound: true, SacctState: "COMPLETED", SacctExit: "0:0"}, true, ""},
		{"completed with exit code", framework.SlurmJobInfo{ID: "7", SacctFound: true, SacctState: "COMPLETED", SacctExit: "1:0"}, true, "COMPLETED with exit code 1:0"},
		{"cancelled by", framework.SlurmJobInfo{ID: "7", SacctFound: true, SacctState: "CANCELLED by 0", SacctExit: "0:15"}, true, "CANCELLED by 0"},
		{"node fail", framework.SlurmJobInfo{ID: "7", SacctFound: true, SacctState: "NODE_FAIL", SacctExit: "0:0"}, true, "NODE_FAIL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, failure := jobOutcome(tt.info)
			assert.Equal(t, tt.wantDone, done)
			if tt.wantFailure == "" {
				assert.Empty(t, failure)
				return
			}
			assert.Contains(t, failure, tt.wantFailure)
		})
	}
}
