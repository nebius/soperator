package updatecontroller

import (
	"sync"
	"time"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"nebius.ai/slurm-operator/internal/consts"
)

const (
	waitBudget   = "budget_exhausted"
	waitReboot   = "reboot_pending"
	waitHandoff  = "handoff_pending"
	waitPods     = "pods_not_ready"
	waitSlurm    = "slurm_unavailable"
	waitError    = "reconcile_error"
	waitEviction = "eviction_pending"
	waitSafety   = "safety_pending"
	waitMissing  = "slurm_node_missing"
	waitCleanup  = "cleanup_pending"
)

var rolloutWaitReasons = []string{waitBudget, waitReboot, waitHandoff, waitPods, waitSlurm, waitError, waitEviction, waitSafety, waitMissing, waitCleanup}

type rolloutMetricLabels [3]string

type rolloutMetricObject struct {
	uid         types.UID
	labels      rolloutMetricLabels
	state       rolloutState
	workerCount int
}

type rolloutMetrics struct {
	outdated, slots, progress, active, waiting *prometheus.GaugeVec
	workers                                    *prometheus.GaugeVec
	mu                                         sync.Mutex
	objects                                    map[types.NamespacedName]rolloutMetricObject
}

var defaultRolloutMetrics = newRolloutMetrics(metrics.Registry)

func newRolloutMetrics(reg prometheus.Registerer) *rolloutMetrics {
	labels := []string{"resource_namespace", "slurm_cluster", "nodeset"}
	newGauge := func(name, help string, labels []string) *prometheus.GaugeVec {
		gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels)
		reg.MustRegister(gauge)
		return gauge
	}
	return &rolloutMetrics{
		outdated: newGauge("soperator_rollout_outdated_pods", "Observed pods outside the target rollout revision.", labels),
		slots:    newGauge("soperator_rollout_available_slots", "Remaining concurrent reboot slots, including when no rollout is active; clamped to zero and zero when observation fails.", labels),
		progress: newGauge("soperator_rollout_last_progress_timestamp_seconds", "Unix timestamp of last observed rollout progress; resets on controller restart or object identity change. Use only while rollout_active is one.", labels),
		active:   newGauge("soperator_rollout_active", "One while worker revision, readiness, replacement handoffs, or Slurm cleanup remain incomplete.", labels),
		waiting:  newGauge("soperator_rollout_waiting", "One for the current rollout wait reason; all reasons are zero when complete.", append(append([]string{}, labels...), "reason")),
		workers:  newGauge("soperator_rollout_workers", "Mutually exclusive rollout stages of owned pods; ready means target revision and Pod Ready with no rollout work. waiting_for_pod counts missing desired pods. Unavailable observations report last-known capacity as unknown.", append(append([]string{}, labels...), "stage")),
		objects:  make(map[types.NamespacedName]rolloutMetricObject),
	}
}

func rolloutLabels(sts *kruisev1b1.StatefulSet) rolloutMetricLabels {
	nodeset := sts.Labels[consts.LabelNodeSetKey]
	if owner := metav1.GetControllerOf(sts); owner != nil && owner.Kind == "NodeSet" {
		nodeset = owner.Name
	}
	if nodeset == "" {
		nodeset = sts.Name
	}
	return rolloutMetricLabels{sts.Namespace, sts.Labels[consts.LabelInstanceKey], nodeset}
}

func (m *rolloutMetrics) observe(sts *kruisev1b1.StatefulSet, observation *rolloutObservation, now time.Time, reconcileFailed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := types.NamespacedName{Namespace: sts.Namespace, Name: sts.Name}
	labels := rolloutLabels(sts)
	previous := m.objects[key]
	if previous.uid != sts.UID || previous.labels != labels {
		m.deleteLabels(previous.labels)
		previous = rolloutMetricObject{uid: sts.UID, labels: labels}
	}
	state := observation.advance(sts, previous.state, now, reconcileFailed)
	counts := observation.workerCounts(sts, previous.workerCount, reconcileFailed || observation.failed)
	previous.workerCount = 0
	for _, count := range counts {
		previous.workerCount += count
	}
	previous.state = state
	m.objects[key] = previous
	m.outdated.WithLabelValues(labels[:]...).Set(float64(state.Snapshot.Outdated))
	m.slots.WithLabelValues(labels[:]...).Set(float64(max(0, observation.slots)))
	m.progress.WithLabelValues(labels[:]...).Set(float64(state.LastProgress))
	active := 0.0
	if state.Active {
		active = 1
	}
	m.active.WithLabelValues(labels[:]...).Set(active)
	m.setWaiting(labels, observation.wait)
	m.setWorkerCounts(labels, counts)
}

func (m *rolloutMetrics) setWorkerCounts(labels rolloutMetricLabels, counts [workerStageCount]int) {
	for stage, name := range workerStageNames {
		m.workers.WithLabelValues(append(labels[:], name)...).Set(float64(counts[stage]))
	}
}

func (m *rolloutMetrics) setWaiting(labels rolloutMetricLabels, reason string) {
	for _, candidate := range rolloutWaitReasons {
		value := 0.0
		if reason == candidate {
			value = 1
		}
		m.waiting.WithLabelValues(append(labels[:], candidate)...).Set(value)
	}
}

// A failed GET must not leave the last successful observation looking healthy.
func (m *rolloutMetrics) readFailed(key types.NamespacedName) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if object, ok := m.objects[key]; ok {
		m.slots.WithLabelValues(object.labels[:]...).Set(0)
		m.setWaiting(object.labels, waitError)
		var counts [workerStageCount]int
		counts[workerUnknown] = object.workerCount
		m.setWorkerCounts(object.labels, counts)
	}
}

func (m *rolloutMetrics) deleteLabels(labels rolloutMetricLabels) {
	for _, gauge := range []*prometheus.GaugeVec{m.outdated, m.slots, m.progress, m.active} {
		gauge.DeleteLabelValues(labels[:]...)
	}
	for _, reason := range rolloutWaitReasons {
		m.waiting.DeleteLabelValues(append(labels[:], reason)...)
	}
	for _, stage := range workerStageNames {
		m.workers.DeleteLabelValues(append(labels[:], stage)...)
	}
}

func (m *rolloutMetrics) forget(key types.NamespacedName) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if object, ok := m.objects[key]; ok {
		m.deleteLabels(object.labels)
		delete(m.objects, key)
	}
}
