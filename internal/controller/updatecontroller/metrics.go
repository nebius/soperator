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
	metricLabelController        = "controller"
	metricLabelResourceNamespace = "resource_namespace"
	metricLabelSlurmCluster      = "slurm_cluster"
	metricLabelNodeSet           = "nodeset"
	metricLabelReason            = "reason"
	metricLabelStage             = "stage"
)

// Values of the reason label on rollout_waiting.
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

var rolloutWaitReasonLabelValues = []string{waitBudget, waitReboot, waitHandoff, waitPods, waitSlurm, waitError, waitEviction, waitSafety, waitMissing, waitCleanup}

type rolloutLabelValues struct {
	resourceNamespace string
	slurmCluster      string
	nodeSet           string
}

func (v rolloutLabelValues) prometheusLabels() prometheus.Labels {
	return prometheus.Labels{
		metricLabelResourceNamespace: v.resourceNamespace,
		metricLabelSlurmCluster:      v.slurmCluster,
		metricLabelNodeSet:           v.nodeSet,
	}
}

type rolloutMetricObject struct {
	uid         types.UID
	labelValues rolloutLabelValues
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
	resourceLabelNames := []string{metricLabelResourceNamespace, metricLabelSlurmCluster, metricLabelNodeSet}
	newGauge := func(metricName, help string, labelNames []string) *prometheus.GaugeVec {
		gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   "soperator",
			Name:        metricName,
			Help:        help,
			ConstLabels: prometheus.Labels{metricLabelController: RollingUpdateControllerName},
		}, labelNames)
		reg.MustRegister(gauge)
		return gauge
	}
	return &rolloutMetrics{
		outdated: newGauge("rollout_outdated_pods", "Observed pods outside the target rollout revision.", resourceLabelNames),
		slots:    newGauge("rollout_available_handoff_slots", "Remaining concurrent worker handoff slots, including when no rollout is active; clamped to zero and zero when observation fails.", resourceLabelNames),
		progress: newGauge("rollout_last_progress_timestamp_seconds", "Unix timestamp of last observed rollout progress; resets on controller restart or object identity change. Use only while soperator_rollout_active is one.", resourceLabelNames),
		active:   newGauge("rollout_active", "One while worker revision, readiness, replacement handoffs, or Slurm cleanup remain incomplete.", resourceLabelNames),
		waiting:  newGauge("rollout_waiting", "One for the current rollout wait reason; all reasons are zero when complete.", append(append([]string{}, resourceLabelNames...), metricLabelReason)),
		workers:  newGauge("rollout_workers", "Mutually exclusive rollout stages of owned pods; ready means target revision and Pod Ready with no rollout work. waiting_for_pod counts missing desired pods. Unavailable observations report last-known capacity as unknown.", append(append([]string{}, resourceLabelNames...), metricLabelStage)),
		objects:  make(map[types.NamespacedName]rolloutMetricObject),
	}
}

func rolloutLabels(sts *kruisev1b1.StatefulSet) rolloutLabelValues {
	nodeset := sts.Labels[consts.LabelNodeSetKey]
	if owner := metav1.GetControllerOf(sts); owner != nil && owner.Kind == "NodeSet" {
		nodeset = owner.Name
	}
	if nodeset == "" {
		nodeset = sts.Name
	}
	return rolloutLabelValues{
		resourceNamespace: sts.Namespace,
		slurmCluster:      sts.Labels[consts.LabelInstanceKey],
		nodeSet:           nodeset,
	}
}

func (m *rolloutMetrics) observe(sts *kruisev1b1.StatefulSet, observation *rolloutObservation, now time.Time, reconcileFailed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := types.NamespacedName{Namespace: sts.Namespace, Name: sts.Name}
	labelValues := rolloutLabels(sts)
	previous := m.objects[key]
	if previous.uid != sts.UID || previous.labelValues != labelValues {
		m.deleteLabels(previous.labelValues)
		previous = rolloutMetricObject{uid: sts.UID, labelValues: labelValues}
	}
	state := observation.advance(sts, previous.state, now, reconcileFailed)
	counts := observation.workerCounts(sts, previous.workerCount, reconcileFailed || observation.failed)
	previous.workerCount = 0
	for _, count := range counts {
		previous.workerCount += count
	}
	previous.state = state
	m.objects[key] = previous
	labels := labelValues.prometheusLabels()
	m.outdated.With(labels).Set(float64(state.Snapshot.Outdated))
	m.slots.With(labels).Set(float64(max(0, observation.slots)))
	m.progress.With(labels).Set(float64(state.LastProgress))
	active := 0.0
	if state.Active {
		active = 1
	}
	m.active.With(labels).Set(active)
	m.setWaiting(labelValues, observation.wait)
	m.setWorkerCounts(labelValues, counts)
}

func (m *rolloutMetrics) setWorkerCounts(labelValues rolloutLabelValues, counts [workerStageCount]int) {
	labels := labelValues.prometheusLabels()
	for stage, stageLabelValue := range workerStageLabelValues {
		labels[metricLabelStage] = stageLabelValue
		m.workers.With(labels).Set(float64(counts[stage]))
	}
}

func (m *rolloutMetrics) setWaiting(labelValues rolloutLabelValues, reason string) {
	labels := labelValues.prometheusLabels()
	for _, reasonLabelValue := range rolloutWaitReasonLabelValues {
		value := 0.0
		if reason == reasonLabelValue {
			value = 1
		}
		labels[metricLabelReason] = reasonLabelValue
		m.waiting.With(labels).Set(value)
	}
}

// A failed GET must not leave the last successful observation looking healthy.
func (m *rolloutMetrics) readFailed(key types.NamespacedName) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if object, ok := m.objects[key]; ok {
		m.slots.With(object.labelValues.prometheusLabels()).Set(0)
		m.setWaiting(object.labelValues, waitError)
		var counts [workerStageCount]int
		counts[workerUnknown] = object.workerCount
		m.setWorkerCounts(object.labelValues, counts)
	}
}

func (m *rolloutMetrics) deleteLabels(labelValues rolloutLabelValues) {
	labels := labelValues.prometheusLabels()
	for _, gauge := range []*prometheus.GaugeVec{m.outdated, m.slots, m.progress, m.active} {
		gauge.Delete(labels)
	}
	reasonLabels := labelValues.prometheusLabels()
	for _, reason := range rolloutWaitReasonLabelValues {
		reasonLabels[metricLabelReason] = reason
		m.waiting.Delete(reasonLabels)
	}
	stageLabels := labelValues.prometheusLabels()
	for _, stage := range workerStageLabelValues {
		stageLabels[metricLabelStage] = stage
		m.workers.Delete(stageLabels)
	}
}

func (m *rolloutMetrics) forget(key types.NamespacedName) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if object, ok := m.objects[key]; ok {
		m.deleteLabels(object.labelValues)
		delete(m.objects, key)
	}
}
