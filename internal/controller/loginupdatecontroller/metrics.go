package loginupdatecontroller

import (
	"strings"
	"sync"
	"time"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"nebius.ai/slurm-operator/internal/consts"
)

const (
	waitNotice         = "notice_period"
	waitPods           = "pods_not_ready"
	waitTargetRevision = "target_revision_pending"
	waitError          = "reconcile_error"
)

const (
	podStateReady = iota
	podStateNoticePeriod
	podStateRestartEligible
	podStateReplacing
	podStateNotReady
	podStateAwaitingObservation
)

var rolloutWaitReasons = []string{waitNotice, waitPods, waitTargetRevision, waitError}

type observedRollout struct {
	cluster string
	pods    map[string]struct{}
}

type rolloutMetrics struct {
	active, outdated, nextRestart, waiting, podState *prometheus.GaugeVec
	mu                                               sync.Mutex
	objects                                          map[types.NamespacedName]observedRollout
}

var defaultRolloutMetrics = newRolloutMetrics(metrics.Registry)

func newRolloutMetrics(reg prometheus.Registerer) *rolloutMetrics {
	newGauge := func(name, help string, extraLabels ...string) *prometheus.GaugeVec {
		gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   "soperator",
			Subsystem:   "login_rollout",
			Name:        name,
			Help:        help,
			ConstLabels: prometheus.Labels{"controller": ControllerName},
		}, append([]string{"resource_namespace", "slurm_cluster"}, extraLabels...))
		reg.MustRegister(gauge)
		return gauge
	}
	return &rolloutMetrics{
		active:      newGauge("active", "One while the observed login revision, readiness, or replacement remains incomplete."),
		outdated:    newGauge("outdated_pods", "Observed login pods outside the target revision."),
		nextRestart: newGauge("next_restart_timestamp_seconds", "Earliest observed scheduled restart of a nonterminating outdated login pod, including overdue schedules; zero when none. Early requests and unhealthy pods may restart sooner."),
		waiting:     newGauge("waiting", "One for the current delayed login rollout wait reason; all reasons are zero when complete.", "reason"),
		podState:    newGauge("pod_state", "Observed login pod rollout state: 0 ready, 1 notice period, 2 restart eligible, 3 replacing, 4 not ready, 5 awaiting revision or schedule.", "login_pod"),
		objects:     make(map[types.NamespacedName]observedRollout),
	}
}

func rolloutLabels(namespace, cluster string) prometheus.Labels {
	return prometheus.Labels{"resource_namespace": namespace, "slurm_cluster": cluster}
}

func (m *rolloutMetrics) observe(sts *kruisev1b1.StatefulSet, pods []corev1.Pod, now time.Time, waitReason string, reconcileFailed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := types.NamespacedName{Namespace: sts.Namespace, Name: sts.Name}
	cluster := sts.Labels[consts.LabelInstanceKey]
	previous, exists := m.objects[key]
	if exists && previous.cluster != cluster {
		m.deleteLabels(sts.Namespace, previous.cluster)
		previous.pods = nil
	}
	observedPods := make(map[string]struct{}, len(pods))
	activePodName, _, _ := strings.Cut(sts.Annotations[consts.AnnotationLoginUpdateInProgress], "/")
	podLabels := rolloutLabels(sts.Namespace, cluster)

	active := len(pods) != int(ptr.Deref(sts.Spec.Replicas, 0)) ||
		sts.Annotations[consts.AnnotationLoginUpdateInProgress] != "" || waitReason == waitTargetRevision
	outdated := 0
	var nextRestart time.Time
	for i := range pods {
		pod := &pods[i]
		observedPods[pod.Name] = struct{}{}
		podLabels["login_pod"] = pod.Name
		m.podState.With(podLabels).Set(loginPodRolloutState(sts, pod, activePodName, now))
		if pod.DeletionTimestamp != nil || !podReady(pod) {
			active = true
		}
		if sts.Status.UpdateRevision == "" || pod.Labels[appsv1.StatefulSetRevisionLabel] == sts.Status.UpdateRevision {
			continue
		}
		outdated++
		active = true
		if pod.DeletionTimestamp != nil {
			continue
		}
		scheduledAt, err := time.Parse(time.RFC3339Nano, pod.Annotations[consts.AnnotationLoginUpdateScheduledAt])
		if err == nil && (nextRestart.IsZero() || scheduledAt.Before(nextRestart)) {
			nextRestart = scheduledAt
		}
	}
	if activePodName != "" {
		if _, exists := observedPods[activePodName]; !exists {
			observedPods[activePodName] = struct{}{}
			podLabels["login_pod"] = activePodName
			m.podState.With(podLabels).Set(podStateReplacing)
		}
	}
	for name := range previous.pods {
		if _, exists := observedPods[name]; !exists {
			podLabels["login_pod"] = name
			m.podState.Delete(podLabels)
		}
	}
	m.objects[key] = observedRollout{cluster: cluster, pods: observedPods}
	labels := rolloutLabels(sts.Namespace, cluster)
	m.outdated.With(labels).Set(float64(outdated))
	activeValue := 0.0
	if active {
		activeValue = 1
	}
	m.active.With(labels).Set(activeValue)
	nextRestartValue := 0.0
	if !nextRestart.IsZero() {
		nextRestartValue = float64(nextRestart.Unix())
	}
	m.nextRestart.With(labels).Set(nextRestartValue)
	if reconcileFailed {
		waitReason = waitError
	} else if active && waitReason == "" {
		waitReason = waitPods
	}
	m.setWaiting(labels, waitReason)
}

func loginPodRolloutState(sts *kruisev1b1.StatefulSet, pod *corev1.Pod, activePodName string, now time.Time) float64 {
	if pod.DeletionTimestamp != nil || pod.Name == activePodName {
		return podStateReplacing
	}
	if sts.Status.ObservedGeneration < sts.Generation || sts.Status.UpdateRevision == "" {
		return podStateAwaitingObservation
	}
	if pod.Labels[appsv1.StatefulSetRevisionLabel] == sts.Status.UpdateRevision {
		if podReady(pod) {
			return podStateReady
		}
		return podStateNotReady
	}
	if !podReady(pod) || pod.Annotations[consts.AnnotationLoginUpdateRequested] == string(pod.UID) {
		return podStateRestartEligible
	}
	scheduledAt, err := time.Parse(time.RFC3339Nano, pod.Annotations[consts.AnnotationLoginUpdateScheduledAt])
	if err != nil {
		return podStateAwaitingObservation
	}
	if now.Before(scheduledAt) {
		return podStateNoticePeriod
	}
	return podStateRestartEligible
}

func (m *rolloutMetrics) setWaiting(labels prometheus.Labels, reason string) {
	for _, candidate := range rolloutWaitReasons {
		value := 0.0
		if candidate == reason {
			value = 1
		}
		labels["reason"] = candidate
		m.waiting.With(labels).Set(value)
	}
}

func (m *rolloutMetrics) readFailed(key types.NamespacedName) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if observed, ok := m.objects[key]; ok {
		m.setWaiting(rolloutLabels(key.Namespace, observed.cluster), waitError)
	}
}

func (m *rolloutMetrics) deleteLabels(namespace, cluster string) {
	labels := rolloutLabels(namespace, cluster)
	for _, gauge := range []*prometheus.GaugeVec{m.active, m.outdated, m.nextRestart} {
		gauge.Delete(labels)
	}
	m.waiting.DeletePartialMatch(labels)
	m.podState.DeletePartialMatch(labels)
}

func (m *rolloutMetrics) forget(key types.NamespacedName) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if observed, ok := m.objects[key]; ok {
		m.deleteLabels(key.Namespace, observed.cluster)
		delete(m.objects, key)
	}
}
