package sharedsteps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/cucumber/godog"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

const (
	loginUpdateMarker       = "SOPERATOR_E2E_LOGIN_UPDATE"
	loginUpdateReadyTimeout = 10 * time.Minute
	loginUpdatePollInterval = time.Second
)

type loginUpdateSchedule struct {
	Announced time.Time
	Restart   time.Time
}

type LoginUpdates struct {
	loginWorkload
	original    *kubeobjects.SlurmClusterLogin
	statefulSet string
	baseline    map[string]string
	previous    string
	target      string
	schedules   map[string]loginUpdateSchedule
	probePod    corev1.Pod
}

func NewLoginUpdates(info *framework.ClusterInfo, runtime framework.Runtime, kubectl *framework.KubectlClient) *LoginUpdates {
	return &LoginUpdates{loginWorkload: loginWorkload{info: info, runtime: runtime, kubectl: kubectl}}
}

func (s *LoginUpdates) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the login workload is ready for a delayed update test$`, s.prepareLoginUpdate)
	sc.Step(`^a login template update is scheduled with one minute of notice$`, func(ctx context.Context) error {
		return s.scheduleLoginUpdate(ctx, time.Minute, false)
	})
	sc.Step(`^a login template update is scheduled for the next cron window$`, func(ctx context.Context) error {
		return s.scheduleLoginUpdate(ctx, time.Minute, true)
	})
	sc.Step(`^a login template update is scheduled with ten minutes of notice$`, func(ctx context.Context) error {
		return s.scheduleLoginUpdate(ctx, 10*time.Minute, false)
	})
	sc.Step(`^the outdated login pods remain until their scheduled restart$`, s.waitForLoginUpdateDeadline)
	sc.Step(`^the updated login pods become ready$`, s.updatedLoginPodsReady)
	sc.Step(`^outdated login nodes warn only in interactive Bash SSH sessions$`, s.checkLoginUpdateWarnings)
	sc.Step(`^an administrator requests an early login restart$`, s.requestEarlyLoginUpdate)
	sc.Step(`^the selected login pod is replaced before its scheduled restart$`, s.waitForEarlyLoginUpdate)
	sc.Step(`^the selected login pod becomes ready at the target revision$`, s.selectedLoginPodReady)
}

func (s *LoginUpdates) readLoginUpdateState(ctx context.Context) (kubeobjects.LoginStatefulSet, []corev1.Pod, error) {
	sts, err := s.loginStatefulSet(ctx)
	if err != nil {
		return sts, nil, err
	}
	pods, err := s.loginPods(ctx)
	return sts, pods, err
}

func (s *LoginUpdates) waitForLoginUpdateReady(ctx context.Context, restart bool) (kubeobjects.LoginStatefulSet, []corev1.Pod, error) {
	var observed kubeobjects.LoginStatefulSet
	var observedPods []corev1.Pod
	err := s.runtime.WaitFor(ctx, "login workload ready at its target revision", loginUpdateReadyTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			sts, pods, err := s.readLoginUpdateState(waitCtx)
			if err != nil {
				return false, err
			}
			if restart && sts.Status.ObservedGeneration >= sts.Metadata.Generation && sts.Status.UpdateRevision != "" {
				if err := s.requestLoginPodUpdates(waitCtx, sts, pods); err != nil {
					return false, err
				}
			}
			if err := loginRolloutReady(sts, pods, "", false); err != nil {
				return false, err
			}
			if sts.Metadata.Annotations["slurm.nebius.ai/login-update-in-progress"] != "" {
				return false, fmt.Errorf("wait for login StatefulSet %s to clear its active replacement", sts.Metadata.Name)
			}
			for _, pod := range pods {
				for _, key := range []string{loginUpdateAnnounced, loginUpdateScheduled, loginUpdateRequested} {
					if pod.Annotations[key] != "" {
						return false, fmt.Errorf("wait for login pod %s to clear %s", pod.Name, key)
					}
				}
			}
			observed, observedPods = sts, pods
			return true, nil
		})
	return observed, observedPods, err
}

func (s *LoginUpdates) prepareLoginUpdate(ctx context.Context) error {
	var cluster kubeobjects.SlurmCluster
	if err := s.kubectl.GetJSON(ctx, &cluster, "get", "slurmcluster", s.info.SlurmClusterName,
		"-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return fmt.Errorf("read login update configuration: %w", err)
	}
	login := cluster.Spec.SlurmNodes.Login
	autoscaling, err := loginFeatureEnabled(login.Autoscaling)
	if err != nil {
		return err
	}
	if login.Size == 0 && !autoscaling {
		s.runtime.Logf("Login nodes are disabled; skipping login updates")
		return godog.ErrSkip
	}
	s.original = &login
	sts, _, err := s.waitForLoginUpdateReady(ctx, true)
	if err != nil {
		return err
	}
	s.statefulSet = sts.Metadata.Name
	patch := map[string]any{"updateStrategy": "delayedRollout"}
	if autoscaling {
		patch["size"] = *sts.Spec.Replicas
		patch["autoscaling"] = map[string]any{"enabled": false}
	}
	if err := s.patchLogin(ctx, patch); err != nil {
		return err
	}
	if autoscaling {
		if err := s.waitLoginHPA(ctx, s.statefulSet, false, loginUpdateReadyTimeout); err != nil {
			return err
		}
	}
	if err := s.runtime.WaitFor(ctx, "login StatefulSet uses delayed replacement", time.Minute, loginUpdatePollInterval,
		func(waitCtx context.Context) (bool, error) {
			current, err := s.loginStatefulSet(waitCtx)
			if err != nil {
				return false, err
			}
			if current.Spec.UpdateStrategy.Type != string(appsv1.OnDeleteStatefulSetStrategyType) || current.Status.ObservedGeneration < current.Metadata.Generation {
				return false, fmt.Errorf("wait for OnDelete login strategy: type=%s generation=%d observed=%d",
					current.Spec.UpdateStrategy.Type, current.Metadata.Generation, current.Status.ObservedGeneration)
			}
			return true, nil
		}); err != nil {
		return err
	}
	sts, pods, err := s.waitForLoginUpdateReady(ctx, false)
	if err != nil {
		return err
	}
	s.previous, s.baseline = sts.Status.UpdateRevision, snapshotPods(pods)
	return nil
}

func (s *LoginUpdates) scheduleLoginUpdate(ctx context.Context, notice time.Duration, cron bool) error {
	var cluster kubeobjects.SlurmCluster
	if err := s.kubectl.GetJSON(ctx, &cluster, "get", "slurmcluster", s.info.SlurmClusterName,
		"-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return err
	}
	var env []corev1.EnvVar
	for _, variable := range cluster.Spec.SlurmNodes.Login.Sshd.CustomEnv {
		if variable.Name != loginUpdateMarker {
			env = append(env, variable)
		}
	}
	env = append(env, corev1.EnvVar{Name: loginUpdateMarker, Value: strconv.FormatInt(time.Now().UnixNano(), 10)})
	policy := map[string]any{"minNotice": notice.String(), "schedule": nil}
	if cron {
		policy["schedule"] = "* * * * *"
	}
	if err := s.patchLogin(ctx, map[string]any{"sshd": map[string]any{"customEnv": env}, "updatePolicy": policy}); err != nil {
		return err
	}
	return s.runtime.WaitFor(ctx, "new login revision announced without replacing existing pods", time.Minute, loginUpdatePollInterval,
		func(waitCtx context.Context) (bool, error) {
			sts, pods, err := s.readLoginUpdateState(waitCtx)
			if err != nil {
				return false, err
			}
			for name, uid := range s.baseline {
				index := slices.IndexFunc(pods, func(pod corev1.Pod) bool { return pod.Name == name })
				if index < 0 || string(pods[index].UID) != uid || pods[index].DeletionTimestamp != nil {
					return false, &framework.PermanentError{Err: fmt.Errorf("login pod %s (uid=%s) was replaced before its update schedule was observed", name, uid)}
				}
			}
			if sts.Status.ObservedGeneration < sts.Metadata.Generation || sts.Status.UpdateRevision == "" || sts.Status.UpdateRevision == s.previous {
				return false, fmt.Errorf("wait for new login revision: previous=%s generation=%d status=%+v", s.previous, sts.Metadata.Generation, sts.Status)
			}
			schedules := make(map[string]loginUpdateSchedule, len(pods))
			for _, pod := range pods {
				if pod.Labels[appsv1.StatefulSetRevisionLabel] == sts.Status.UpdateRevision {
					return false, fmt.Errorf("login pod %s already has target revision %s", pod.Name, sts.Status.UpdateRevision)
				}
				schedule, err := loginUpdatePodSchedule(pod, notice, cron)
				if err != nil {
					return false, err
				}
				schedules[pod.Name] = schedule
			}
			if len(schedules) != len(s.baseline) {
				return false, fmt.Errorf("login pod count changed: got %d, expected %d", len(schedules), len(s.baseline))
			}
			s.target, s.schedules, s.probePod = sts.Status.UpdateRevision, schedules, pods[0]
			for name, schedule := range schedules {
				s.runtime.Logf("Login update %s: announced=%s restart=%s cron=%t", name,
					schedule.Announced.Format(time.RFC3339Nano), schedule.Restart.Format(time.RFC3339Nano), cron)
			}
			return true, nil
		})
}

func loginUpdatePodSchedule(pod corev1.Pod, notice time.Duration, cron bool) (loginUpdateSchedule, error) {
	announced, err := time.Parse(time.RFC3339Nano, pod.Annotations[loginUpdateAnnounced])
	if err != nil {
		return loginUpdateSchedule{}, fmt.Errorf("read login pod %s announcement: %w", pod.Name, err)
	}
	restart, err := time.Parse(time.RFC3339Nano, pod.Annotations[loginUpdateScheduled])
	if err != nil {
		return loginUpdateSchedule{}, fmt.Errorf("read login pod %s restart time: %w", pod.Name, err)
	}
	expected := announced.Add(notice)
	if cron && !expected.Equal(expected.Truncate(time.Minute)) {
		expected = expected.Truncate(time.Minute).Add(time.Minute)
	}
	if !restart.Equal(expected) {
		return loginUpdateSchedule{}, fmt.Errorf("login pod %s restart=%s, expected %s after notice=%s cron=%t", pod.Name,
			restart.Format(time.RFC3339Nano), expected.Format(time.RFC3339Nano), notice, cron)
	}
	return loginUpdateSchedule{Announced: announced, Restart: restart}, nil
}

func loginUpdateDeletionStart(pod corev1.Pod) time.Time {
	if pod.DeletionTimestamp == nil {
		return time.Time{}
	}
	started := pod.DeletionTimestamp.Time
	if grace := pod.DeletionGracePeriodSeconds; grace != nil && *grace > 0 {
		started = started.Add(-time.Duration(*grace) * time.Second)
	}
	return started
}

func loginUpdatesReachedDeadline(baseline map[string]string, schedules map[string]loginUpdateSchedule, pods []corev1.Pod, now time.Time) (bool, error) {
	allDue := true
	for name, uid := range baseline {
		restart := schedules[name].Restart
		if now.Before(restart) {
			allDue = false
		}
		index := slices.IndexFunc(pods, func(pod corev1.Pod) bool { return pod.Name == name })
		if index < 0 || string(pods[index].UID) != uid {
			if now.Before(restart) {
				return false, fmt.Errorf("login pod %s (uid=%s) was replaced before restart=%s (now=%s)", name, uid, restart, now)
			}
			// Kubernetes pod timestamps have second precision; same-second replacements may be on time.
			if index >= 0 && pods[index].CreationTimestamp.Time.Before(restart.Truncate(time.Second)) {
				return false, fmt.Errorf("login pod %s replacement was created at %s before restart=%s", name, pods[index].CreationTimestamp, restart)
			}
			continue
		}
		if deleted := pods[index].DeletionTimestamp; deleted != nil {
			started := loginUpdateDeletionStart(pods[index])
			// The API omits timestamp fractions, including from the end of the grace period.
			if now.Before(restart) || started.Before(restart.Truncate(time.Second)) {
				return false, fmt.Errorf("check login pod %s restart deadline: deletion started=%s precedes restart=%s (deletionTimestamp=%s)", name, started, restart, deleted)
			}
		}
	}
	return allDue, nil
}

func (s *LoginUpdates) waitForLoginUpdateDeadline(ctx context.Context) error {
	return s.runtime.WaitFor(ctx, "login pods preserved until their restart deadlines", 3*time.Minute, loginUpdatePollInterval,
		func(waitCtx context.Context) (bool, error) {
			pods, err := s.loginPods(waitCtx)
			if err != nil {
				return false, err
			}
			due, err := loginUpdatesReachedDeadline(s.baseline, s.schedules, pods, time.Now())
			if err != nil {
				return false, &framework.PermanentError{Err: err}
			}
			return due, nil
		})
}

func (s *LoginUpdates) updatedLoginPodsReady(ctx context.Context) error {
	sts, pods, err := s.waitForLoginUpdateReady(ctx, false)
	if err != nil {
		return err
	}
	if sts.Status.UpdateRevision != s.target {
		return fmt.Errorf("login target revision changed: got %s, expected %s", sts.Status.UpdateRevision, s.target)
	}
	for _, pod := range pods {
		if string(pod.UID) == s.baseline[pod.Name] {
			return fmt.Errorf("login pod %s retained outdated UID %s", pod.Name, pod.UID)
		}
	}
	_, err = loginUpdatesReachedDeadline(s.baseline, s.schedules, pods, time.Now())
	return err
}

func (s *LoginUpdates) requestEarlyLoginUpdate(ctx context.Context) error {
	pod, present, err := s.kubectl.PodIfPresent(ctx, framework.SoperatorNamespace, s.probePod.Name)
	if err != nil {
		return err
	}
	if !present || pod.UID != s.probePod.UID || pod.DeletionTimestamp != nil {
		return fmt.Errorf("selected login pod %s (uid=%s) changed before its early restart request", s.probePod.Name, s.probePod.UID)
	}
	if !time.Now().Before(s.schedules[pod.Name].Restart) {
		return fmt.Errorf("early restart deadline already reached on login pod %s", pod.Name)
	}
	return s.requestLoginPodUpdate(ctx, pod)
}

func (s *LoginUpdates) waitForEarlyLoginUpdate(ctx context.Context) error {
	return s.runtime.WaitFor(ctx, "selected login pod replacement starts before its restart deadline", 3*time.Minute, loginUpdatePollInterval,
		func(waitCtx context.Context) (bool, error) {
			pod, present, err := s.kubectl.PodIfPresent(waitCtx, framework.SoperatorNamespace, s.probePod.Name)
			if err != nil {
				return false, err
			}
			restart := s.schedules[s.probePod.Name].Restart
			if !present || pod.UID != s.probePod.UID {
				if !time.Now().Before(restart) {
					return false, &framework.PermanentError{Err: fmt.Errorf("login pod %s replacement was not observed before %s", s.probePod.Name, restart)}
				}
				return true, nil
			}
			if pod.DeletionTimestamp != nil {
				started := loginUpdateDeletionStart(pod)
				if !time.Now().Before(restart) && !started.Before(restart.Truncate(time.Second)) {
					return false, &framework.PermanentError{Err: fmt.Errorf("check early restart of login pod %s: deletion started=%s was not before %s", pod.Name, started, restart)}
				}
				return true, nil
			}
			return false, fmt.Errorf("login pod %s still has original UID %s, scheduled=%s", pod.Name, pod.UID, restart)
		})
}

func (s *LoginUpdates) selectedLoginPodReady(ctx context.Context) error {
	return s.runtime.WaitFor(ctx, "selected login pod ready at the target revision", loginUpdateReadyTimeout, framework.DefaultPollInterval,
		func(waitCtx context.Context) (bool, error) {
			pod, present, err := s.kubectl.PodIfPresent(waitCtx, framework.SoperatorNamespace, s.probePod.Name)
			if err != nil {
				return false, err
			}
			if !present || pod.UID == s.probePod.UID || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning ||
				!kubeobjects.PodReady(pod) || pod.Labels[appsv1.StatefulSetRevisionLabel] != s.target {
				return false, fmt.Errorf("wait for login pod %s: present=%t uid=%s phase=%s revision=%s target=%s ready=%t", s.probePod.Name,
					present, pod.UID, pod.Status.Phase, pod.Labels[appsv1.StatefulSetRevisionLabel], s.target, kubeobjects.PodReady(pod))
			}
			return true, nil
		})
}

func loginUpdateRestorePatch(original kubeobjects.SlurmClusterLogin) (map[string]any, error) {
	var policy any
	if len(original.UpdatePolicy) != 0 {
		var fields map[string]any
		if err := json.Unmarshal(original.UpdatePolicy, &fields); err != nil {
			return nil, fmt.Errorf("decode original login update policy: %w", err)
		}
		if fields != nil {
			if _, exists := fields["minNotice"]; !exists {
				fields["minNotice"] = nil
			}
			if _, exists := fields["schedule"]; !exists {
				fields["schedule"] = nil
			}
			policy = fields
		}
	}
	var autoscaling any
	if len(original.Autoscaling) != 0 {
		if err := json.Unmarshal(original.Autoscaling, &autoscaling); err != nil {
			return nil, fmt.Errorf("decode original login autoscaling: %w", err)
		}
	}
	var strategy any
	if original.UpdateStrategy != "" {
		strategy = original.UpdateStrategy
	}
	return map[string]any{"updateStrategy": strategy, "updatePolicy": policy, "size": original.Size, "autoscaling": autoscaling}, nil
}

func (s *LoginUpdates) CleanupAndReset(ctx context.Context) {
	if s.original != nil {
		runCleanupPhase(ctx, s.runtime, "restore login update configuration and readiness", 3*loginUpdateReadyTimeout,
			func(cleanupCtx context.Context) error {
				_, _, rolloutErr := s.waitForLoginUpdateReady(cleanupCtx, true)
				patch, restoreErr := loginUpdateRestorePatch(*s.original)
				if restoreErr == nil {
					restoreErr = s.patchLogin(cleanupCtx, patch)
				}
				if restoreErr == nil && s.statefulSet != "" {
					enabled, err := loginFeatureEnabled(s.original.Autoscaling)
					if err == nil {
						err = s.waitLoginHPA(cleanupCtx, s.statefulSet, enabled, loginUpdateReadyTimeout)
					}
					restoreErr = err
				}
				_, _, readyErr := s.waitForLoginUpdateReady(cleanupCtx, true)
				err := errors.Join(rolloutErr, restoreErr, readyErr)
				framework.RecordWorkerCleanupError(cleanupCtx, err)
				return err
			})
	}
	workload := s.loginWorkload
	*s = LoginUpdates{loginWorkload: workload}
}
