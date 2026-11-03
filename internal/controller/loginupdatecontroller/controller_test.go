package loginupdatecontroller

import (
	"context"
	"fmt"
	"testing"
	"time"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	clocktesting "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

var testNoticeTime = time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC)

func TestReconcileSchedulesAtOrAfterMinimumNotice(t *testing.T) {
	tests := []struct {
		name      string
		announced string
		scheduled string
		policy    *slurmv1.LoginUpdatePolicy
	}{
		{name: "omitted policy uses exact default notice", announced: "2026-10-15T12:00:00Z", scheduled: "2026-11-14T12:00:00Z"},
		{
			name: "notice without cron preserves exact time", announced: "2026-10-15T12:34:56.123456789Z", scheduled: "2026-10-16T12:34:56.123456789Z",
			policy: &slurmv1.LoginUpdatePolicy{MinNotice: &metav1.Duration{Duration: 24 * time.Hour}},
		},
		{name: "skip window inside notice", announced: "2026-10-15T12:00:00Z", scheduled: "2026-12-01T00:00:00Z", policy: &slurmv1.LoginUpdatePolicy{Schedule: ptr.To("0 0 1 * *")}},
		{name: "include exact notice boundary", announced: "2026-09-01T00:00:00Z", scheduled: "2026-10-01T00:00:00Z", policy: &slurmv1.LoginUpdatePolicy{Schedule: ptr.To("0 0 1 * *")}},
		{name: "preserve nanosecond notice boundary", announced: "2026-09-01T00:00:00.000000001Z", scheduled: "2026-11-01T00:00:00Z", policy: &slurmv1.LoginUpdatePolicy{Schedule: ptr.To("0 0 1 * *")}},
		{name: "default to UTC with non-UTC clock", announced: "2026-09-01T02:00:00+02:00", scheduled: "2026-10-01T00:00:00Z", policy: &slurmv1.LoginUpdatePolicy{Schedule: ptr.To("0 0 1 * *")}},
		{
			name: "honor explicit timezone", announced: "2026-10-15T12:00:00Z", scheduled: "2026-10-17T01:00:00Z",
			policy: testClusterPolicy(24*time.Hour, "CRON_TZ=Europe/Amsterdam 0 3 * * *"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t)
			announced, err := time.Parse(time.RFC3339Nano, tt.announced)
			require.NoError(t, err)
			r.clock = clocktesting.NewFakeClock(announced)
			if tt.policy != nil {
				cluster := getLoginCluster(t, r.Client)
				cluster.Spec.SlurmNodes.Login.UpdatePolicy = tt.policy
				require.NoError(t, r.Update(t.Context(), cluster))
			}
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			scheduled, err := time.Parse(time.RFC3339Nano, tt.scheduled)
			require.NoError(t, err)
			assert.Equal(t, scheduled.Sub(announced), result.RequeueAfter)
			for _, pod := range pods {
				current := getLoginPod(t, r.Client, pod)
				assert.Equal(t, tt.scheduled, current.Annotations[consts.AnnotationLoginUpdateScheduledAt])
			}
		})
	}
}

func TestReconcileWithoutCronReplacesAtNoticeDeadline(t *testing.T) {
	r, sts, _ := testLoginReconciler(t)
	cluster := getLoginCluster(t, r.Client)
	cluster.Spec.SlurmNodes.Login.UpdatePolicy = &slurmv1.LoginUpdatePolicy{MinNotice: &metav1.Duration{Duration: time.Hour}}
	require.NoError(t, r.Update(t.Context(), cluster))
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, time.Hour, result.RequeueAfter)
	r.clock = clocktesting.NewFakeClock(testNoticeTime.Add(time.Hour - time.Nanosecond))
	result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, time.Nanosecond, result.RequeueAfter)
	assert.Equal(t, 2, loginPodCount(t, r.Client))
	r.clock = clocktesting.NewFakeClock(testNoticeTime.Add(time.Hour))
	result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, DefaultRequeueAfter, result.RequeueAfter)
	assert.Equal(t, 1, loginPodCount(t, r.Client))
}

func TestReconcileWithoutCronAndZeroNoticeReplacesImmediately(t *testing.T) {
	r, sts, _ := testLoginReconciler(t)
	cluster := getLoginCluster(t, r.Client)
	cluster.Spec.SlurmNodes.Login.UpdatePolicy = &slurmv1.LoginUpdatePolicy{MinNotice: &metav1.Duration{}}
	require.NoError(t, r.Update(t.Context(), cluster))
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, DefaultRequeueAfter, result.RequeueAfter)
	assert.Equal(t, 1, loginPodCount(t, r.Client))
}

func TestReconcilePersistsNoticeAcrossRevisionChangesAndControllerRestarts(t *testing.T) {
	r, sts, pods := testLoginReconciler(t)
	reconcileLogin(t, r, sts)
	for _, pod := range pods {
		current := getLoginPod(t, r.Client, pod)
		assert.Equal(t, testNoticeTime.Format(time.RFC3339), current.Annotations[consts.AnnotationLoginUpdateAnnouncedAt])
		assert.Equal(t, "2026-11-14T12:00:00Z", current.Annotations[consts.AnnotationLoginUpdateScheduledAt])
	}

	currentSTS := &kruisev1b1.StatefulSet{}
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(sts), currentSTS))
	currentSTS.Status.UpdateRevision = "newer-revision"
	require.NoError(t, r.Status().Update(t.Context(), currentSTS))
	r.clock = clocktesting.NewFakeClock(testNoticeTime.Add(10 * 24 * time.Hour))
	reconcileLogin(t, r, sts)
	for _, pod := range pods {
		current := getLoginPod(t, r.Client, pod)
		assert.Equal(t, testNoticeTime.Format(time.RFC3339), current.Annotations[consts.AnnotationLoginUpdateAnnouncedAt])
		assert.Equal(t, "2026-11-14T12:00:00Z", current.Annotations[consts.AnnotationLoginUpdateScheduledAt])
	}

	restarted := NewReconciler(r.Client, r.Client, r.Scheme, events.NewFakeRecorder(20), r.requeueAfter)
	restarted.clock = clocktesting.NewFakeClock(testNoticeTime.Add(30 * 24 * time.Hour))
	reconcileLogin(t, restarted, sts)
	assert.Equal(t, 1, loginPodCount(t, restarted.Client))
	reconcileLogin(t, restarted, sts)
	assert.Equal(t, 1, loginPodCount(t, restarted.Client), "wait for the missing replacement before deleting another pod")
}

func TestReconcileDefersToClusterRollingUpdateBeforeStatefulSetChanges(t *testing.T) {
	r, sts, pods := testLoginReconciler(t)
	reconcileLogin(t, r, sts)
	cluster := getLoginCluster(t, r.Client)
	cluster.Spec.SlurmNodes.Login.UpdateStrategy = consts.UpdateStrategyRollingUpdate
	require.NoError(t, r.Update(t.Context(), cluster))
	r.clock = clocktesting.NewFakeClock(testNoticeTime.Add(30 * 24 * time.Hour))
	for _, original := range pods {
		pod := getLoginPod(t, r.Client, original)
		pod.Annotations[consts.AnnotationLoginUpdateRequested] = string(pod.UID)
		require.NoError(t, r.Update(t.Context(), pod))
	}
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Zero(t, result.RequeueAfter)
	assert.Equal(t, 2, loginPodCount(t, r.Client), "wait for the cluster controller to apply the selected rolling strategy")
}

func TestReconcileEarlyRequestIsScopedToCurrentPodUID(t *testing.T) {
	tests := []struct {
		name        string
		request     string
		wantDeleted bool
	}{
		{"current UID", "login-0-uid", true},
		{"old instance UID", "previous-login-0-uid", false},
		{"no request", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t)
			r.requeueAfter = 17 * time.Second
			pods[0].Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: tt.request}
			require.NoError(t, r.Update(t.Context(), pods[0]))
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			if tt.wantDeleted {
				assert.Equal(t, 17*time.Second, result.RequeueAfter)
			} else {
				assert.Equal(t, 30*24*time.Hour, result.RequeueAfter)
			}
			err = r.Get(t.Context(), client.ObjectKeyFromObject(pods[0]), &corev1.Pod{})
			if tt.wantDeleted {
				assert.True(t, apierrors.IsNotFound(err), "early approval should bypass the scheduled date")
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(pods[1]), &corev1.Pod{}))
		})
	}
}

func TestReconcilePreservesAvailabilityBeforeAnyRestart(t *testing.T) {
	tests := []struct {
		name   string
		change func(*kruisev1b1.StatefulSet, []*corev1.Pod) []*corev1.Pod
	}{
		{"missing desired replica", func(_ *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod { return pods[:1] }},
		{"target-revision pod is unready", func(_ *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod {
			pods[1].Labels[appsv1.ControllerRevisionHashLabelKey] = "new-revision"
			pods[1].Status.Conditions[0].Status = corev1.ConditionFalse
			return pods
		}},
		{"target-revision pod is pending", func(_ *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod {
			pods[1].Labels[appsv1.ControllerRevisionHashLabelKey] = "new-revision"
			pods[1].Status.Phase = corev1.PodPending
			return pods
		}},
		{"another pod is terminating", func(_ *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod {
			pods[1].DeletionTimestamp = ptr.To(metav1.NewTime(testNoticeTime.Add(-time.Second)))
			pods[1].Finalizers = []string{"test/hold-deletion"}
			return pods
		}},
		{"target-revision pod is terminating", func(_ *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod {
			pods[1].Labels[appsv1.ControllerRevisionHashLabelKey] = "new-revision"
			pods[1].DeletionTimestamp = ptr.To(metav1.NewTime(testNoticeTime.Add(-time.Second)))
			pods[1].Finalizers = []string{"test/hold-deletion"}
			return pods
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t, tt.change)
			r.requeueAfter = 17 * time.Second
			requested := getLoginPod(t, r.Client, pods[0])
			requested.Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(requested.UID)}
			require.NoError(t, r.Update(t.Context(), requested))
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, 17*time.Second, result.RequeueAfter)
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(requested), &corev1.Pod{}))
		})
	}
}

func TestReconcileRestartsOnlyOnePodAndWaitsForReadyReplacement(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprintf("broken old pods %t", broken), func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t, func(_ *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod {
				if broken {
					for _, pod := range pods {
						pod.Status.Conditions[0].Status = corev1.ConditionFalse
					}
				}
				return pods
			})
			r.requeueAfter = 17 * time.Second
			for _, pod := range pods {
				pod.Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(pod.UID)}
				require.NoError(t, r.Update(t.Context(), pod))
			}
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, 17*time.Second, result.RequeueAfter)
			assert.Equal(t, 1, loginPodCount(t, r.Client))
			result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, 17*time.Second, result.RequeueAfter)
			assert.Equal(t, 1, loginPodCount(t, r.Client))

			var deleted *corev1.Pod
			for _, pod := range pods {
				if apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(pod), &corev1.Pod{})) {
					deleted = pod
				}
			}
			require.NotNil(t, deleted)
			require.Equal(t, "login-0", deleted.Name, "candidates with the same readiness use lexical name order")
			replacement := deleted.DeepCopy()
			replacement.ResourceVersion = ""
			replacement.UID = types.UID(string(deleted.UID) + "-replacement")
			replacement.Labels[appsv1.ControllerRevisionHashLabelKey] = "new-revision"
			replacement.Annotations = nil
			replacement.Status.Conditions[0].Status = corev1.ConditionFalse
			require.NoError(t, r.Create(t.Context(), replacement))
			result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, 17*time.Second, result.RequeueAfter)
			assert.Equal(t, 2, loginPodCount(t, r.Client))
			replacement = getLoginPod(t, r.Client, replacement)
			replacement.Status.Conditions[0].Status = corev1.ConditionTrue
			require.NoError(t, r.Status().Update(t.Context(), replacement))
			result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, 17*time.Second, result.RequeueAfter)
			assert.Equal(t, 1, loginPodCount(t, r.Client))
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(replacement), &corev1.Pod{}))
		})
	}
}

func TestReconcileKeepsActivePodAcrossRevisionChangesAndControllerRestarts(t *testing.T) {
	for _, peerReady := range []bool{true, false} {
		t.Run(fmt.Sprintf("remaining old pod ready %t", peerReady), func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t)
			reconcileLogin(t, r, sts)
			r.clock = clocktesting.NewFakeClock(testNoticeTime.Add(30 * 24 * time.Hour))
			reconcileLogin(t, r, sts)
			require.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(pods[0]), &corev1.Pod{})))
			if !peerReady {
				peer := getLoginPod(t, r.Client, pods[1])
				peer.Status.Conditions[0].Status = corev1.ConditionFalse
				require.NoError(t, r.Status().Update(t.Context(), peer))
			}

			replacement := testLoginPod(sts, 0)
			replacement.UID = "login-0-second-instance"
			replacement.Labels[appsv1.ControllerRevisionHashLabelKey] = "new-revision"
			replacement.Status.Conditions[0].Status = corev1.ConditionFalse
			require.NoError(t, r.Create(t.Context(), replacement))
			currentSTS := &kruisev1b1.StatefulSet{}
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(sts), currentSTS))
			currentSTS.Status.UpdateRevision = "newer-revision"
			require.NoError(t, r.Status().Update(t.Context(), currentSTS))

			restarted := NewReconciler(r.Client, r.APIReader, r.Scheme, events.NewFakeRecorder(20), r.requeueAfter)
			restarted.clock = r.clock
			reconcileLogin(t, restarted, sts)
			require.True(t, apierrors.IsNotFound(restarted.Get(t.Context(), client.ObjectKeyFromObject(replacement), &corev1.Pod{})), "retry login-0 immediately without another notice period")
			peer := getLoginPod(t, restarted.Client, pods[1])
			assert.Equal(t, pods[1].UID, peer.UID, "do not advance to login-1 when the target revision changes")
			assert.Equal(t, testNoticeTime.Format(time.RFC3339Nano), peer.Annotations[consts.AnnotationLoginUpdateAnnouncedAt])

			replacement = testLoginPod(sts, 0)
			replacement.UID = "login-0-third-instance"
			replacement.Labels[appsv1.ControllerRevisionHashLabelKey] = "newer-revision"
			replacement.Status.Conditions[0].Status = corev1.ConditionFalse
			require.NoError(t, restarted.Create(t.Context(), replacement))
			reconcileLogin(t, restarted, sts)
			assert.Equal(t, 2, loginPodCount(t, restarted.Client), "wait for login-0 to become Ready before replacing login-1")

			replacement = getLoginPod(t, restarted.Client, replacement)
			replacement.Status.Conditions[0].Status = corev1.ConditionTrue
			require.NoError(t, restarted.Status().Update(t.Context(), replacement))
			reconcileLogin(t, restarted, sts)
			require.True(t, apierrors.IsNotFound(restarted.Get(t.Context(), client.ObjectKeyFromObject(peer), &corev1.Pod{})), "advance to login-1 once login-0 is Ready")
			require.NoError(t, restarted.Get(t.Context(), client.ObjectKeyFromObject(replacement), &corev1.Pod{}))
		})
	}
}

func TestReconcileKeepsActiveLoginDuringScaleDownOfAnotherPod(t *testing.T) {
	r, sts, pods := testLoginReconciler(t, func(sts *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod {
		sts.Spec.Replicas = ptr.To(int32(3))
		return append(pods, testLoginPod(sts, 2))
	})
	pods[1].Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(pods[1].UID)}
	require.NoError(t, r.Update(t.Context(), pods[1]))
	reconcileLogin(t, r, sts)
	require.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(pods[1]), &corev1.Pod{})))

	currentSTS := &kruisev1b1.StatefulSet{}
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(sts), currentSTS))
	currentSTS.Spec.Replicas = ptr.To(int32(2))
	require.NoError(t, r.Update(t.Context(), currentSTS))
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, DefaultRequeueAfter, result.RequeueAfter, "login-0 and login-2 do not mean the desired login-1 replacement can be forgotten")

	require.NoError(t, r.Delete(t.Context(), getLoginPod(t, r.Client, pods[2])))
	replacement := testLoginPod(sts, 1)
	replacement.UID = "login-1-replacement"
	replacement.Labels[appsv1.ControllerRevisionHashLabelKey] = "new-revision"
	replacement.Status.Conditions[0].Status = corev1.ConditionFalse
	require.NoError(t, r.Create(t.Context(), replacement))
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(sts), currentSTS))
	currentSTS.Status.UpdateRevision = "newer-revision"
	require.NoError(t, r.Status().Update(t.Context(), currentSTS))
	reconcileLogin(t, r, sts)
	require.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(replacement), &corev1.Pod{})), "continue the same active replacement without another notice")
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(pods[0]), &corev1.Pod{}))
}

func TestReconcileContinuesAfterActiveLoginIsScaledDown(t *testing.T) {
	r, sts, pods := testLoginReconciler(t)
	pods[1].Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(pods[1].UID)}
	require.NoError(t, r.Update(t.Context(), pods[1]))
	reconcileLogin(t, r, sts)
	require.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(pods[1]), &corev1.Pod{})))

	currentSTS := &kruisev1b1.StatefulSet{}
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(sts), currentSTS))
	currentSTS.Spec.Replicas = ptr.To(int32(1))
	require.NoError(t, r.Update(t.Context(), currentSTS))
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, 30*24*time.Hour, result.RequeueAfter, "do not wait for a replacement removed by scale-down")

	r.clock = clocktesting.NewFakeClock(testNoticeTime.Add(30 * 24 * time.Hour))
	reconcileLogin(t, r, sts)
	require.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(pods[0]), &corev1.Pod{})))
	replacement := testLoginPod(sts, 0)
	replacement.UID = "login-0-replacement"
	replacement.Labels[appsv1.ControllerRevisionHashLabelKey] = "new-revision"
	require.NoError(t, r.Create(t.Context(), replacement))
	result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result, "stop reconciling once the final replacement is Ready")
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(sts), currentSTS))
	assert.NotContains(t, currentSTS.Annotations, consts.AnnotationLoginUpdateInProgress)
}

func TestReconcileCanRestartSingleLoginPod(t *testing.T) {
	tests := []struct {
		name  string
		phase corev1.PodPhase
		ready corev1.ConditionStatus
	}{
		{"healthy running", corev1.PodRunning, corev1.ConditionTrue},
		{"unready running", corev1.PodRunning, corev1.ConditionFalse},
		{"pending without a readiness condition", corev1.PodPending, ""},
		{"readiness unknown", corev1.PodRunning, corev1.ConditionUnknown},
		{"failed", corev1.PodFailed, corev1.ConditionFalse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t, func(sts *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod {
				sts.Spec.Replicas = ptr.To(int32(1))
				pods[0].Status.Phase = tt.phase
				if tt.ready == "" {
					pods[0].Status.Conditions = nil
				} else {
					pods[0].Status.Conditions[0].Status = tt.ready
				}
				return pods[:1]
			})
			r.requeueAfter = 17 * time.Second
			pods[0].Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(pods[0].UID)}
			require.NoError(t, r.Update(t.Context(), pods[0]))
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, 17*time.Second, result.RequeueAfter)
			assert.Zero(t, loginPodCount(t, r.Client))
			result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, 17*time.Second, result.RequeueAfter, "wait for the sole login replacement")
		})
	}
}

func TestReconcilePrioritizesBrokenEligiblePod(t *testing.T) {
	r, sts, pods := testLoginReconciler(t, func(_ *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod {
		pods[1].Status.Conditions[0].Status = corev1.ConditionFalse
		return pods
	})
	for _, pod := range pods {
		pod.Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(pod.UID)}
		require.NoError(t, r.Update(t.Context(), pod))
	}
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, DefaultRequeueAfter, result.RequeueAfter)
	assert.Equal(t, 1, loginPodCount(t, r.Client))
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(pods[0]), &corev1.Pod{}))
	assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(pods[1]), &corev1.Pod{})), "broken login-1 must precede healthy login-0")
}

func TestBrokenPodsStillRequireDeadlineOrEarlyRequest(t *testing.T) {
	for _, earlyRequest := range []bool{false, true} {
		t.Run(fmt.Sprintf("early request %t", earlyRequest), func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t, func(_ *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod {
				pods[0].Status.Phase = corev1.PodFailed
				pods[1].Status.Phase = corev1.PodPending
				for _, pod := range pods {
					pod.Status.Conditions[0].Status = corev1.ConditionFalse
				}
				return pods
			})
			cluster := getLoginCluster(t, r.Client)
			cluster.Spec.SlurmNodes.Login.UpdatePolicy = &slurmv1.LoginUpdatePolicy{MinNotice: &metav1.Duration{Duration: time.Hour}}
			require.NoError(t, r.Update(t.Context(), cluster))
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, time.Hour, result.RequeueAfter)
			assert.Equal(t, 2, loginPodCount(t, r.Client))
			deleted := pods[0]
			if earlyRequest {
				requested := getLoginPod(t, r.Client, pods[1])
				requested.Annotations[consts.AnnotationLoginUpdateRequested] = string(requested.UID)
				require.NoError(t, r.Update(t.Context(), requested))
				deleted = pods[1]
			} else {
				r.clock = clocktesting.NewFakeClock(testNoticeTime.Add(time.Hour))
			}
			result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, DefaultRequeueAfter, result.RequeueAfter)
			assert.Equal(t, 1, loginPodCount(t, r.Client))
			assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(deleted), &corev1.Pod{})))
		})
	}
}

func TestReconcileDoesNotActWithoutObservedTargetRevision(t *testing.T) {
	tests := []struct {
		name         string
		change       func(*kruisev1b1.StatefulSet)
		expectedWait time.Duration
	}{
		{"no target revision", func(sts *kruisev1b1.StatefulSet) { sts.Status.UpdateRevision = "" }, 17 * time.Second},
		{"unobserved generation", func(sts *kruisev1b1.StatefulSet) { sts.Generation++ }, 17 * time.Second},
		{"automatic rollout enabled", func(sts *kruisev1b1.StatefulSet) {
			sts.Spec.UpdateStrategy.Type = appsv1.RollingUpdateStatefulSetStrategyType
		}, 0},
		{"non-login component", func(sts *kruisev1b1.StatefulSet) { sts.Labels[consts.LabelComponentKey] = "worker" }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t, func(sts *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod {
				tt.change(sts)
				for _, pod := range pods {
					pod.Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(pod.UID)}
				}
				return pods
			})
			r.requeueAfter = 17 * time.Second
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, tt.expectedWait, result.RequeueAfter)
			for _, original := range pods {
				pod := getLoginPod(t, r.Client, original)
				assert.Empty(t, pod.Annotations[consts.AnnotationLoginUpdateAnnouncedAt])
				assert.Empty(t, pod.Annotations[consts.AnnotationLoginUpdateScheduledAt])
			}
		})
	}
}

func TestReconcileIgnoresForeignStatefulSets(t *testing.T) {
	tests := []struct {
		name   string
		change func(*kruisev1b1.StatefulSet)
	}{
		{"missing cluster owner", func(sts *kruisev1b1.StatefulSet) { sts.OwnerReferences = nil }},
		{"foreign owner kind", func(sts *kruisev1b1.StatefulSet) { sts.OwnerReferences[0].Kind = "Deployment" }},
		{"foreign owner api", func(sts *kruisev1b1.StatefulSet) { sts.OwnerReferences[0].APIVersion = "another.example/v1" }},
		{"owner is not controller", func(sts *kruisev1b1.StatefulSet) { sts.OwnerReferences[0].Controller = ptr.To(false) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t, func(sts *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod {
				tt.change(sts)
				for _, pod := range pods {
					pod.Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(pod.UID)}
				}
				return pods
			})
			reconcileLogin(t, r, sts)
			for _, original := range pods {
				current := getLoginPod(t, r.Client, original)
				assert.Empty(t, current.Annotations[consts.AnnotationLoginUpdateAnnouncedAt])
				assert.Empty(t, current.Annotations[consts.AnnotationLoginUpdateScheduledAt])
				assert.Equal(t, string(current.UID), current.Annotations[consts.AnnotationLoginUpdateRequested])
			}
		})
	}
}

func TestReconcileDoesNotRestartCurrentRevisionAndClearsStaleMetadata(t *testing.T) {
	r, sts, pods := testLoginReconciler(t)
	current := pods[0]
	current.Labels[appsv1.ControllerRevisionHashLabelKey] = "new-revision"
	current.Annotations = map[string]string{
		consts.AnnotationLoginUpdateAnnouncedAt: "2026-01-01T00:00:00Z",
		consts.AnnotationLoginUpdateScheduledAt: "2026-02-06T00:00:00Z",
		consts.AnnotationLoginUpdateRequested:   string(current.UID),
		"customer.example/keep":                 "value",
	}
	require.NoError(t, r.Update(t.Context(), current))
	reconcileLogin(t, r, sts)
	current = getLoginPod(t, r.Client, current)
	assert.NotContains(t, current.Annotations, consts.AnnotationLoginUpdateAnnouncedAt)
	assert.NotContains(t, current.Annotations, consts.AnnotationLoginUpdateScheduledAt)
	assert.NotContains(t, current.Annotations, consts.AnnotationLoginUpdateRequested)
	assert.Equal(t, "value", current.Annotations["customer.example/keep"])
}

func TestReconcileIgnoresForeignPodsWithMatchingLabels(t *testing.T) {
	r, sts, pods := testLoginReconciler(t)
	pods[0].Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(pods[0].UID)}
	require.NoError(t, r.Update(t.Context(), pods[0]))
	foreign := testLoginPod(sts, 9)
	foreign.OwnerReferences[0].UID = "another-statefulset-uid"
	foreign.Status.Conditions[0].Status = corev1.ConditionFalse
	foreign.Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(foreign.UID)}
	require.NoError(t, r.Create(t.Context(), foreign))
	reconcileLogin(t, r, sts)
	assert.Equal(t, 2, loginPodCount(t, r.Client), "a foreign unready pod must not block a ready login restart")
	current := getLoginPod(t, r.Client, foreign)
	assert.Empty(t, current.Annotations[consts.AnnotationLoginUpdateAnnouncedAt])
	assert.Empty(t, current.Annotations[consts.AnnotationLoginUpdateScheduledAt])
	assert.Equal(t, string(foreign.UID), current.Annotations[consts.AnnotationLoginUpdateRequested])
}

func TestReconcileDeletesWithPodIdentityAndVersionPreconditions(t *testing.T) {
	r, sts, pods := testLoginReconciler(t)
	pods[0].Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(pods[0].UID)}
	require.NoError(t, r.Update(t.Context(), pods[0]))
	var deleted bool
	r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			deleted = true
			options := (&client.DeleteOptions{}).ApplyOptions(opts)
			require.NotNil(t, options.Preconditions)
			require.NotNil(t, options.Preconditions.UID)
			require.NotNil(t, options.Preconditions.ResourceVersion)
			assert.Equal(t, obj.GetUID(), *options.Preconditions.UID)
			assert.NotEmpty(t, *options.Preconditions.ResourceVersion)
			assert.Equal(t, obj.GetResourceVersion(), *options.Preconditions.ResourceVersion)
			return c.Delete(ctx, obj, opts...)
		},
	})
	reconcileLogin(t, r, sts)
	assert.True(t, deleted)
}

func TestReconcileUsesIndependentClusterPolicies(t *testing.T) {
	r, sts, pods := testLoginReconciler(t)
	r = NewReconciler(r.Client, r.APIReader, r.Scheme, r.Recorder, 7*time.Second)
	r.clock = clocktesting.NewFakeClock(testNoticeTime)
	cluster := getLoginCluster(t, r.Client)
	cluster.Spec.SlurmNodes.Login.UpdatePolicy = testClusterPolicy(24*time.Hour, "0 3 * * *")
	require.NoError(t, r.Update(t.Context(), cluster))

	otherCluster := cluster.DeepCopy()
	otherCluster.Name = "other-cluster"
	otherCluster.UID = "other-cluster-uid"
	otherCluster.ResourceVersion = ""
	otherCluster.Spec.SlurmNodes.Login.UpdatePolicy = testClusterPolicy(48*time.Hour, "0 6 * * *")
	require.NoError(t, r.Create(t.Context(), otherCluster))
	otherSTS := sts.DeepCopy()
	otherSTS.Name = "other-login"
	otherSTS.UID = "other-login-uid"
	otherSTS.ResourceVersion = ""
	otherSTS.Labels[consts.LabelInstanceKey] = otherCluster.Name
	otherSTS.OwnerReferences[0].Name = otherCluster.Name
	otherSTS.OwnerReferences[0].UID = otherCluster.UID
	otherSTS.Spec.Selector.MatchLabels["test-cluster"] = otherCluster.Name
	require.NoError(t, r.Create(t.Context(), otherSTS))
	otherPods := []*corev1.Pod{testLoginPod(otherSTS, 0), testLoginPod(otherSTS, 1)}
	for _, pod := range otherPods {
		require.NoError(t, r.Create(t.Context(), pod))
	}

	for _, tt := range []struct {
		sts       *kruisev1b1.StatefulSet
		pods      []*corev1.Pod
		scheduled string
	}{
		{sts, pods, "2026-10-17T03:00:00Z"},
		{otherSTS, otherPods, "2026-10-18T06:00:00Z"},
	} {
		result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(tt.sts)})
		require.NoError(t, err)
		scheduled, err := time.Parse(time.RFC3339, tt.scheduled)
		require.NoError(t, err)
		assert.Equal(t, scheduled.Sub(testNoticeTime), result.RequeueAfter)
		for _, pod := range tt.pods {
			current := getLoginPod(t, r.Client, pod)
			assert.Equal(t, tt.scheduled, current.Annotations[consts.AnnotationLoginUpdateScheduledAt])
		}
	}
}

func TestPolicyEditsRecalculateExistingPodsAndApplyToNewPods(t *testing.T) {
	r, sts, pods := testLoginReconciler(t)
	reconcileLogin(t, r, sts)
	cluster := getLoginCluster(t, r.Client)
	cluster.Generation++
	cluster.Spec.SlurmNodes.Login.UpdatePolicy = testClusterPolicy(24*time.Hour, "0 3 * * *")
	require.NoError(t, r.Update(t.Context(), cluster))
	r.clock = clocktesting.NewFakeClock(testNoticeTime.Add(24 * time.Hour))
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, 15*time.Hour, result.RequeueAfter, "apply the new policy to the original announcement")
	for _, pod := range pods {
		current := getLoginPod(t, r.Client, pod)
		assert.Equal(t, testNoticeTime.Format(time.RFC3339), current.Annotations[consts.AnnotationLoginUpdateAnnouncedAt])
		assert.Equal(t, "2026-10-17T03:00:00Z", current.Annotations[consts.AnnotationLoginUpdateScheduledAt])
	}

	require.NoError(t, r.Delete(t.Context(), getLoginPod(t, r.Client, pods[1])))
	newPod := testLoginPod(sts, 1)
	newPod.UID = "new-login-instance-uid"
	require.NoError(t, r.Create(t.Context(), newPod))
	result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, 15*time.Hour, result.RequeueAfter, "the earlier existing announcement still determines the next wakeup")
	newPod = getLoginPod(t, r.Client, newPod)
	assert.Equal(t, "2026-10-16T12:00:00Z", newPod.Annotations[consts.AnnotationLoginUpdateAnnouncedAt])
	assert.Equal(t, "2026-10-18T03:00:00Z", newPod.Annotations[consts.AnnotationLoginUpdateScheduledAt])
}

func TestPolicyEditsRecalculateFromOriginalAnnouncement(t *testing.T) {
	tests := []struct {
		name     string
		before   *slurmv1.LoginUpdatePolicy
		after    *slurmv1.LoginUpdatePolicy
		expected string
	}{
		{"shorten notice", nil, &slurmv1.LoginUpdatePolicy{MinNotice: &metav1.Duration{Duration: 10 * 24 * time.Hour}}, "2026-10-25T12:00:00Z"},
		{"extend notice", nil, &slurmv1.LoginUpdatePolicy{MinNotice: &metav1.Duration{Duration: 45 * 24 * time.Hour}}, "2026-11-29T12:00:00Z"},
		{"add cron", nil, &slurmv1.LoginUpdatePolicy{Schedule: ptr.To("0 0 1 * *")}, "2026-12-01T00:00:00Z"},
		{"change cron", testClusterPolicy(24*time.Hour, "0 3 * * *"), testClusterPolicy(24*time.Hour, "0 6 * * *"), "2026-10-17T06:00:00Z"},
		{"remove cron", &slurmv1.LoginUpdatePolicy{Schedule: ptr.To("0 0 1 * *")}, nil, "2026-11-14T12:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t)
			cluster := getLoginCluster(t, r.Client)
			cluster.Spec.SlurmNodes.Login.UpdatePolicy = tt.before
			require.NoError(t, r.Update(t.Context(), cluster))
			reconcileLogin(t, r, sts)
			cluster = getLoginCluster(t, r.Client)
			cluster.Spec.SlurmNodes.Login.UpdatePolicy = tt.after
			require.NoError(t, r.Update(t.Context(), cluster))
			now := testNoticeTime.Add(24 * time.Hour)
			r.clock = clocktesting.NewFakeClock(now)
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			expected, err := time.Parse(time.RFC3339, tt.expected)
			require.NoError(t, err)
			assert.Equal(t, expected.Sub(now), result.RequeueAfter)
			for _, pod := range pods {
				current := getLoginPod(t, r.Client, pod)
				assert.Equal(t, map[string]string{
					consts.AnnotationLoginUpdateAnnouncedAt: testNoticeTime.Format(time.RFC3339Nano),
					consts.AnnotationLoginUpdateScheduledAt: tt.expected,
				}, current.Annotations)
			}
		})
	}
}

func TestPolicyShorteningMakesPastDeadlineImmediatelyEligible(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprintf("unready target-revision peer %t", blocked), func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t)
			reconcileLogin(t, r, sts)
			cluster := getLoginCluster(t, r.Client)
			cluster.Spec.SlurmNodes.Login.UpdatePolicy = &slurmv1.LoginUpdatePolicy{MinNotice: &metav1.Duration{Duration: 12 * time.Hour}}
			require.NoError(t, r.Update(t.Context(), cluster))
			if blocked {
				peer := getLoginPod(t, r.Client, pods[1])
				peer.Labels[appsv1.ControllerRevisionHashLabelKey] = "new-revision"
				require.NoError(t, r.Update(t.Context(), peer))
				peer.Status.Conditions[0].Status = corev1.ConditionFalse
				require.NoError(t, r.Status().Update(t.Context(), peer))
			}
			r.clock = clocktesting.NewFakeClock(testNoticeTime.Add(24 * time.Hour))
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, DefaultRequeueAfter, result.RequeueAfter)
			if blocked {
				assert.Equal(t, 2, loginPodCount(t, r.Client))
			} else {
				assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(pods[0]), &corev1.Pod{})))
				assert.Equal(t, 1, loginPodCount(t, r.Client))
			}
			remainingOld := pods[1]
			if blocked {
				remainingOld = pods[0]
				peer := getLoginPod(t, r.Client, pods[1])
				assert.Empty(t, peer.Annotations, "target-revision pods clear their old schedules")
			}
			current := getLoginPod(t, r.Client, remainingOld)
			assert.Equal(t, testNoticeTime.Format(time.RFC3339Nano), current.Annotations[consts.AnnotationLoginUpdateAnnouncedAt])
			assert.Equal(t, "2026-10-16T00:00:00Z", current.Annotations[consts.AnnotationLoginUpdateScheduledAt])
		})
	}
}

func TestPolicyRecalculationPreservesAnnotationsAndEarlyRequests(t *testing.T) {
	r, sts, pods := testLoginReconciler(t)
	const originalAnnouncement = "2026-10-15T14:00:00+02:00"
	pods[0].Annotations = map[string]string{
		consts.AnnotationLoginUpdateAnnouncedAt: originalAnnouncement,
		consts.AnnotationLoginUpdateScheduledAt: "2026-11-14T12:00:00Z",
		"customer.example/keep":                 "value",
	}
	require.NoError(t, r.Update(t.Context(), pods[0]))
	reconcileLogin(t, r, sts)
	requested := getLoginPod(t, r.Client, pods[0])
	requested.Annotations[consts.AnnotationLoginUpdateRequested] = string(requested.UID)
	require.NoError(t, r.Update(t.Context(), requested))
	peer := getLoginPod(t, r.Client, pods[1])
	peer.Labels[appsv1.ControllerRevisionHashLabelKey] = "new-revision"
	require.NoError(t, r.Update(t.Context(), peer))
	peer.Status.Conditions[0].Status = corev1.ConditionFalse
	require.NoError(t, r.Status().Update(t.Context(), peer))
	cluster := getLoginCluster(t, r.Client)
	cluster.Spec.SlurmNodes.Login.UpdatePolicy = &slurmv1.LoginUpdatePolicy{MinNotice: &metav1.Duration{Duration: 45 * 24 * time.Hour}}
	require.NoError(t, r.Update(t.Context(), cluster))
	r.clock = clocktesting.NewFakeClock(testNoticeTime.Add(24 * time.Hour))
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, DefaultRequeueAfter, result.RequeueAfter)
	requested = getLoginPod(t, r.Client, requested)
	scheduled, err := time.Parse(time.RFC3339Nano, requested.Annotations[consts.AnnotationLoginUpdateScheduledAt])
	require.NoError(t, err)
	assert.True(t, scheduled.Equal(testNoticeTime.Add(45*24*time.Hour)))
	assert.Equal(t, map[string]string{
		consts.AnnotationLoginUpdateAnnouncedAt: originalAnnouncement,
		consts.AnnotationLoginUpdateScheduledAt: requested.Annotations[consts.AnnotationLoginUpdateScheduledAt],
		consts.AnnotationLoginUpdateRequested:   string(requested.UID),
		"customer.example/keep":                 "value",
	}, requested.Annotations)
	peer = getLoginPod(t, r.Client, pods[1])
	peer.Status.Conditions[0].Status = corev1.ConditionTrue
	require.NoError(t, r.Status().Update(t.Context(), peer))
	result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, DefaultRequeueAfter, result.RequeueAfter)
	assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(requested), &corev1.Pod{})))
}

func TestUnchangedDeadlineDoesNotPatchPods(t *testing.T) {
	for _, explicitDefaults := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit defaults %t", explicitDefaults), func(t *testing.T) {
			r, sts, _ := testLoginReconciler(t)
			reconcileLogin(t, r, sts)
			if explicitDefaults {
				cluster := getLoginCluster(t, r.Client)
				cluster.Spec.SlurmNodes.Login.UpdatePolicy = &slurmv1.LoginUpdatePolicy{MinNotice: &metav1.Duration{Duration: 30 * 24 * time.Hour}}
				require.NoError(t, r.Update(t.Context(), cluster))
			}
			var patches int
			r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					patches++
					return c.Patch(ctx, obj, patch, opts...)
				},
			})
			r.clock = clocktesting.NewFakeClock(testNoticeTime.Add(time.Hour))
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Zero(t, patches)
			assert.Equal(t, 30*24*time.Hour-time.Hour, result.RequeueAfter)
		})
	}
}

func TestReconcileBlocksWhenParentClusterIsUnavailable(t *testing.T) {
	tests := []struct {
		name   string
		change func(*testing.T, client.Client, *slurmv1.SlurmCluster)
	}{
		{"missing cluster", func(t *testing.T, c client.Client, cluster *slurmv1.SlurmCluster) {
			require.NoError(t, c.Delete(t.Context(), cluster))
		}},
		{"deleting cluster", func(t *testing.T, c client.Client, cluster *slurmv1.SlurmCluster) {
			cluster.Finalizers = []string{"test/hold-deletion"}
			require.NoError(t, c.Update(t.Context(), cluster))
			require.NoError(t, c.Delete(t.Context(), cluster))
		}},
		{"recycled cluster name", func(t *testing.T, c client.Client, cluster *slurmv1.SlurmCluster) {
			require.NoError(t, c.Delete(t.Context(), cluster))
			cluster.ResourceVersion = ""
			cluster.UID = "new-cluster-instance-uid"
			require.NoError(t, c.Create(t.Context(), cluster))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t)
			cluster := getLoginCluster(t, r.Client)
			tt.change(t, r.Client, cluster)
			for _, pod := range pods {
				pod.Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(pod.UID)}
				require.NoError(t, r.Update(t.Context(), pod))
			}
			reconcileLogin(t, r, sts)
			for _, pod := range pods {
				current := getLoginPod(t, r.Client, pod)
				assert.Empty(t, current.Annotations[consts.AnnotationLoginUpdateAnnouncedAt])
				assert.Empty(t, current.Annotations[consts.AnnotationLoginUpdateScheduledAt])
			}
		})
	}
}

func TestReconcileRejectsInvalidRuntimeSchedule(t *testing.T) {
	r, sts, pods := testLoginReconciler(t)
	cluster := getLoginCluster(t, r.Client)
	cluster.Spec.SlurmNodes.Login.UpdatePolicy = testClusterPolicy(time.Hour, "invalid")
	require.NoError(t, r.Update(t.Context(), cluster))
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.Error(t, err)
	assert.Equal(t, ctrl.Result{}, result)
	for _, pod := range pods {
		current := getLoginPod(t, r.Client, pod)
		assert.Empty(t, current.Annotations[consts.AnnotationLoginUpdateAnnouncedAt])
	}
}

func TestReconcileWaitsForEarliestDeadlineFromOriginalAnnouncements(t *testing.T) {
	tests := []struct {
		name     string
		earliest time.Duration
		later    time.Duration
	}{
		{"deadline beyond retry interval", 72 * time.Hour, 96 * time.Hour},
		{"deadline sooner than retry interval", 15 * time.Second, 2 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t)
			for i, delay := range []time.Duration{tt.later, tt.earliest} {
				pods[i].Annotations = map[string]string{
					consts.AnnotationLoginUpdateAnnouncedAt: testNoticeTime.Add(delay - 30*24*time.Hour).Format(time.RFC3339Nano),
					consts.AnnotationLoginUpdateScheduledAt: testNoticeTime.Add(delay).Format(time.RFC3339Nano),
				}
				require.NoError(t, r.Update(t.Context(), pods[i]))
			}
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, tt.earliest, result.RequeueAfter)
			r.clock = clocktesting.NewFakeClock(testNoticeTime.Add(5 * time.Second))
			result, err = r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, tt.earliest-5*time.Second, result.RequeueAfter)
			for _, pod := range pods {
				current := getLoginPod(t, r.Client, pod)
				assert.Equal(t, pod.Annotations, current.Annotations, "unchanged policy must preserve published deadlines")
			}
		})
	}
}

func TestReconcileStopsRequeueingWhenNoRolloutIsPending(t *testing.T) {
	for _, staleMetadata := range []bool{false, true} {
		t.Run(fmt.Sprintf("stale metadata %t", staleMetadata), func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t)
			for _, pod := range pods {
				pod.Labels[appsv1.ControllerRevisionHashLabelKey] = "new-revision"
				if staleMetadata {
					pod.Annotations = map[string]string{
						consts.AnnotationLoginUpdateAnnouncedAt: "2026-01-01T00:00:00Z",
						consts.AnnotationLoginUpdateScheduledAt: "2026-02-01T00:00:00Z",
						consts.AnnotationLoginUpdateRequested:   string(pod.UID),
					}
				}
				require.NoError(t, r.Update(t.Context(), pod))
			}
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, ctrl.Result{}, result)
			for _, pod := range pods {
				current := getLoginPod(t, r.Client, pod)
				assert.Empty(t, current.Annotations)
			}
		})
	}
	t.Run("zero desired replicas", func(t *testing.T) {
		r, sts, _ := testLoginReconciler(t, func(sts *kruisev1b1.StatefulSet, _ []*corev1.Pod) []*corev1.Pod {
			sts.Spec.Replicas = ptr.To(int32(0))
			return nil
		})
		result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
		require.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)
	})
}

func TestReconcileDoesNotAdvanceWhenReplacementFails(t *testing.T) {
	for _, failMarking := range []bool{true, false} {
		t.Run(fmt.Sprintf("fail recording active pod %t", failMarking), func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t)
			pods[0].Annotations = map[string]string{consts.AnnotationLoginUpdateRequested: string(pods[0].UID)}
			require.NoError(t, r.Update(t.Context(), pods[0]))
			r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, isStatefulSet := obj.(*kruisev1b1.StatefulSet); isStatefulSet && failMarking {
						return assert.AnError
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
				Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
					require.False(t, failMarking, "record the active pod before attempting deletion")
					return assert.AnError
				},
			})
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.ErrorIs(t, err, assert.AnError)
			assert.Equal(t, ctrl.Result{}, result)
			assert.Equal(t, 2, loginPodCount(t, r.Client))
		})
	}
}

func TestClusterRequestsSelectsMatchingOwnedLoginStatefulSets(t *testing.T) {
	r, sts, _ := testLoginReconciler(t)
	cluster := getLoginCluster(t, r.Client)
	for _, tt := range []struct {
		name   string
		change func(*kruisev1b1.StatefulSet)
	}{
		{"recycled-owner", func(sts *kruisev1b1.StatefulSet) { sts.OwnerReferences[0].UID = "previous-cluster-uid" }},
		{"another-cluster", func(sts *kruisev1b1.StatefulSet) {
			sts.OwnerReferences[0].Name = "another-cluster"
			sts.OwnerReferences[0].UID = "another-cluster-uid"
			sts.Labels[consts.LabelInstanceKey] = "another-cluster"
		}},
	} {
		other := sts.DeepCopy()
		other.Name = tt.name
		other.UID = types.UID(tt.name + "-uid")
		other.ResourceVersion = ""
		tt.change(other)
		require.NoError(t, r.Create(t.Context(), other))
	}
	assert.Equal(t, []ctrl.Request{{NamespacedName: client.ObjectKeyFromObject(sts)}}, r.clusterRequests(t.Context(), cluster))
}

func testClusterPolicy(notice time.Duration, schedule string) *slurmv1.LoginUpdatePolicy {
	return &slurmv1.LoginUpdatePolicy{
		MinNotice: &metav1.Duration{Duration: notice},
		Schedule:  ptr.To(schedule),
	}
}

func getLoginCluster(t *testing.T, c client.Client) *slurmv1.SlurmCluster {
	t.Helper()
	cluster := &slurmv1.SlurmCluster{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "cluster"}, cluster))
	return cluster
}

func testLoginReconciler(t *testing.T, changes ...func(*kruisev1b1.StatefulSet, []*corev1.Pod) []*corev1.Pod) (*Reconciler, *kruisev1b1.StatefulSet, []*corev1.Pod) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, kruisev1b1.AddToScheme(scheme))
	require.NoError(t, slurmv1.AddToScheme(scheme))
	sts := &kruisev1b1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "login", Namespace: "default", UID: "login-statefulset-uid", Generation: 1,
			Labels: map[string]string{
				consts.LabelNameKey:      consts.LabelNameValue,
				consts.LabelManagedByKey: consts.LabelManagedByValue,
				consts.LabelInstanceKey:  "cluster",
				consts.LabelComponentKey: "login",
			},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: slurmv1.GroupVersion.String(), Kind: slurmv1.KindSlurmCluster, Name: "cluster", UID: "cluster-uid", Controller: ptr.To(true)}},
		},
		Spec: kruisev1b1.StatefulSetSpec{
			Replicas:       ptr.To(int32(2)),
			Selector:       &metav1.LabelSelector{MatchLabels: map[string]string{consts.LabelComponentKey: "login", "test-cluster": "cluster"}},
			UpdateStrategy: kruisev1b1.StatefulSetUpdateStrategy{Type: appsv1.OnDeleteStatefulSetStrategyType},
		},
		Status: kruisev1b1.StatefulSetStatus{ObservedGeneration: 1, Replicas: 2, ReadyReplicas: 2, UpdateRevision: "new-revision"},
	}
	pods := []*corev1.Pod{testLoginPod(sts, 0), testLoginPod(sts, 1)}
	for _, change := range changes {
		pods = change(sts, pods)
	}
	cluster := &slurmv1.SlurmCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "default", UID: "cluster-uid", Generation: 1}}
	objects := []client.Object{sts, cluster}
	for _, pod := range pods {
		objects = append(objects, pod)
	}
	kubeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(sts, &corev1.Pod{}).WithObjects(objects...).Build()
	r := NewReconciler(kubeClient, kubeClient, scheme, events.NewFakeRecorder(20), DefaultRequeueAfter)
	r.clock = clocktesting.NewFakeClock(testNoticeTime)
	return r, sts, pods
}

func testLoginPod(sts *kruisev1b1.StatefulSet, ordinal int) *corev1.Pod {
	name := fmt.Sprintf("%s-%d", sts.Name, ordinal)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: sts.Namespace, UID: types.UID(name + "-uid"),
			Labels:          map[string]string{consts.LabelComponentKey: "login", "test-cluster": sts.Labels[consts.LabelInstanceKey], appsv1.ControllerRevisionHashLabelKey: "old-revision"},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: kruisev1b1.SchemeGroupVersion.String(), Kind: "StatefulSet", Name: sts.Name, UID: sts.UID, Controller: ptr.To(true)}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
}

func reconcileLogin(t *testing.T, r *Reconciler, sts *kruisev1b1.StatefulSet) {
	t.Helper()
	_, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
}

func getLoginPod(t *testing.T, c client.Client, original *corev1.Pod) *corev1.Pod {
	t.Helper()
	pod := &corev1.Pod{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(original), pod))
	return pod
}

func loginPodCount(t *testing.T, c client.Client) int {
	t.Helper()
	pods := &corev1.PodList{}
	require.NoError(t, c.List(t.Context(), pods, client.InNamespace("default")))
	return len(pods.Items)
}
