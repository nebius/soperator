package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
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

func TestLoginUpdatePodSchedule(t *testing.T) {
	for _, tc := range []struct {
		name, announced, restart string
		cron, invalid            bool
	}{
		{name: "exact notice", announced: "2026-11-24T12:34:56.123456789Z", restart: "2026-11-24T12:35:56.123456789Z"},
		{name: "cron round up", cron: true, announced: "2026-11-24T12:34:56.123456789Z", restart: "2026-11-24T12:36:00Z"},
		{name: "cron exact boundary", cron: true, announced: "2026-11-24T12:34:00Z", restart: "2026-11-24T12:35:00Z"},
		{name: "cron hour rollover", cron: true, announced: "2026-11-24T12:58:59Z", restart: "2026-11-24T13:00:00Z"},
		{name: "cron day rollover", cron: true, announced: "2026-11-24T23:58:59Z", restart: "2026-11-25T00:00:00Z"},
		{name: "missing announcement", restart: "2026-11-24T12:35:00Z", invalid: true},
		{name: "invalid announcement", announced: "yesterday", restart: "2026-11-24T12:35:00Z", invalid: true},
		{name: "missing restart", announced: "2026-11-24T12:34:00Z", invalid: true},
		{name: "invalid restart", announced: "2026-11-24T12:34:00Z", restart: "tomorrow", invalid: true},
		{name: "notice too short", announced: "2026-11-24T12:34:00Z", restart: "2026-11-24T12:34:59Z", invalid: true},
		{name: "wrong cron occurrence", cron: true, announced: "2026-11-24T12:34:00Z", restart: "2026-11-24T12:36:00Z", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "login-0", Annotations: map[string]string{
				loginUpdateAnnounced: tc.announced, loginUpdateScheduled: tc.restart,
			}}}
			schedule, err := loginUpdatePodSchedule(pod, time.Minute, tc.cron)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.announced, schedule.Announced.Format(time.RFC3339Nano))
			assert.Equal(t, tc.restart, schedule.Restart.Format(time.RFC3339Nano))
		})
	}
}

func TestLoginUpdatesReachedDeadline(t *testing.T) {
	restart := time.Date(2026, time.November, 24, 12, 35, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                           string
		now, deletion, creation        time.Duration
		deadlineFraction               time.Duration
		grace                          int64
		replaced, missing, terminating bool
		due, invalid                   bool
	}{
		{name: "waiting", now: -time.Second},
		{name: "exact deadline", due: true},
		{name: "after deadline", now: time.Minute, due: true},
		{name: "replacement before deadline", now: -time.Second, replaced: true, invalid: true},
		{name: "missing before deadline", now: -time.Second, missing: true, invalid: true},
		{name: "deletion before deadline", now: -time.Second, terminating: true, deletion: -time.Second, invalid: true},
		{name: "termination grace crosses deadline", now: -time.Second, terminating: true, deletion: 30 * time.Second, invalid: true},
		{name: "early deletion observed after deadline", now: time.Minute, terminating: true, deletion: -time.Second, invalid: true},
		{name: "early graceful deletion observed after deadline", now: time.Second, terminating: true, deletion: 25 * time.Second, grace: 30, invalid: true},
		{name: "graceful deletion at deadline", now: time.Second, terminating: true, deletion: 30 * time.Second, grace: 30, due: true},
		{name: "graceful deletion at fractional deadline", deadlineFraction: 500 * time.Millisecond, now: time.Second, terminating: true, deletion: 30 * time.Second, grace: 30, due: true},
		{name: "graceful deletion after deadline", now: 10 * time.Second, terminating: true, deletion: 35 * time.Second, grace: 30, due: true},
		{name: "deletion at deadline", now: time.Minute, terminating: true, due: true},
		{name: "early replacement observed after deadline", now: time.Minute, replaced: true, creation: -time.Second, invalid: true},
		{name: "replacement at deadline", now: time.Minute, replaced: true, due: true},
		{name: "missing after deadline", now: time.Minute, missing: true, due: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deadline := restart.Add(tc.deadlineFraction)
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "login-0", UID: "original"}}
			if tc.replaced {
				pod.UID = types.UID("replacement")
				pod.CreationTimestamp = metav1.NewTime(deadline.Add(tc.creation).Truncate(time.Second))
			}
			if tc.terminating {
				deleted := metav1.NewTime(deadline.Add(tc.deletion).Truncate(time.Second))
				pod.DeletionTimestamp = &deleted
				if tc.grace > 0 {
					pod.DeletionGracePeriodSeconds = &tc.grace
				}
			}
			var pods []corev1.Pod
			if !tc.missing {
				pods = append(pods, pod)
			}
			due, err := loginUpdatesReachedDeadline(map[string]string{"login-0": "original"},
				map[string]loginUpdateSchedule{"login-0": {Restart: deadline}}, pods, deadline.Add(tc.now))
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.due, due)
		})
	}
}

func TestLoginUpdateRestorePatch(t *testing.T) {
	for _, tc := range []struct {
		name, strategy, policy, autoscaling, expected string
	}{
		{name: "absent fields", expected: `{"updateStrategy":null,"updatePolicy":null,"size":2,"autoscaling":null}`},
		{name: "explicit nulls", policy: "null", autoscaling: "null", expected: `{"updateStrategy":null,"updatePolicy":null,"size":2,"autoscaling":null}`},
		{name: "empty policy", policy: `{}`, expected: `{"updateStrategy":null,"updatePolicy":{"minNotice":null,"schedule":null},"size":2,"autoscaling":null}`},
		{name: "partial policy", strategy: "rollingUpdate", policy: `{"schedule":"0 1 * * *"}`, autoscaling: `{"enabled":true,"minReplicas":2,"maxReplicas":4}`,
			expected: `{"updateStrategy":"rollingUpdate","updatePolicy":{"minNotice":null,"schedule":"0 1 * * *"},"size":2,"autoscaling":{"enabled":true,"minReplicas":2,"maxReplicas":4}}`},
		{name: "complete policy", strategy: "delayedRollout", policy: `{"minNotice":"24h","schedule":"0 1 * * *"}`, autoscaling: `{"enabled":false}`,
			expected: `{"updateStrategy":"delayedRollout","updatePolicy":{"minNotice":"24h","schedule":"0 1 * * *"},"size":2,"autoscaling":{"enabled":false}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patch, err := loginUpdateRestorePatch(kubeobjects.SlurmClusterLogin{
				Size: 2, UpdateStrategy: tc.strategy, UpdatePolicy: json.RawMessage(tc.policy), Autoscaling: json.RawMessage(tc.autoscaling),
			})
			require.NoError(t, err)
			data, err := json.Marshal(patch)
			require.NoError(t, err)
			assert.JSONEq(t, tc.expected, string(data))
		})
	}
	for _, original := range []kubeobjects.SlurmClusterLogin{
		{UpdatePolicy: json.RawMessage(`{`)}, {Autoscaling: json.RawMessage(`{`)},
	} {
		_, err := loginUpdateRestorePatch(original)
		require.Error(t, err)
	}
}

func TestLoginUpdateCleanupRestoresCancelledPartialSetupAfterRolloutFailure(t *testing.T) {
	failure := errors.New("finish pending rollout")
	runtime := &loginUpdateCleanupRuntime{rolloutError: failure}
	info := &framework.ClusterInfo{SlurmClusterName: "soperator"}
	s := NewLoginUpdates(info, runtime, framework.NewKubectlClient(runtime))
	s.original = &kubeobjects.SlurmClusterLogin{Size: 2, UpdateStrategy: "rollingUpdate"}
	ctx, cancel := context.WithCancel(framework.WithWorkerCleanup(t.Context()))
	cancel()

	s.CleanupAndReset(ctx)

	assert.False(t, runtime.cancelled)
	assert.Equal(t, []string{"finish rollout", "restore config", "ready"}, runtime.events)
	assert.JSONEq(t, `{"spec":{"slurmNodes":{"login":{"updateStrategy":"rollingUpdate","updatePolicy":null,"size":2,"autoscaling":null}}}}`, runtime.patch)
	require.ErrorIs(t, framework.FinishWorkerReservation(ctx, runtime), failure)
	assert.Nil(t, s.original)
	assert.Same(t, info, s.info)
	s.CleanupAndReset(ctx)
	assert.Len(t, runtime.events, 3, "cleanup must be idle after reset")
}

type loginUpdateCleanupRuntime struct {
	framework.Runtime
	rolloutError error
	events       []string
	patch        string
	cancelled    bool
}

func (r *loginUpdateCleanupRuntime) Logf(string, ...any) {}

func (r *loginUpdateCleanupRuntime) WaitFor(ctx context.Context, _ string, _, _ time.Duration, _ func(context.Context) (bool, error)) error {
	r.cancelled = r.cancelled || ctx.Err() != nil
	if len(r.events) == 0 {
		r.events = append(r.events, "finish rollout")
		return r.rolloutError
	}
	r.events = append(r.events, "ready")
	return ctx.Err()
}

func (r *loginUpdateCleanupRuntime) Kubectl() framework.ArgsScope {
	return framework.NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		r.cancelled = r.cancelled || ctx.Err() != nil
		if len(args) == 0 || args[0] != "patch" {
			return "", errors.New("unexpected cleanup command")
		}
		r.events = append(r.events, "restore config")
		r.patch = args[len(args)-1]
		return "patched", ctx.Err()
	})
}
