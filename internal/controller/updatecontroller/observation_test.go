package updatecontroller

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clocktesting "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

type metricFixture struct {
	r                    *RollingUpdateReconciler
	registry             *prometheus.Registry
	clock                *clocktesting.FakeClock
	nodes                []slurmapi.Node
	listErr              error
	key, podKey, nodeKey client.ObjectKey
}

func newMetricFixture(t *testing.T) *metricFixture {
	t.Helper()
	slurm := &slurmapifake.MockClient{}
	r, sts, pod, node := testK8sNodeRolloutReconciler(t, slurm)
	node.Spec.Unschedulable = false
	require.NoError(t, r.Update(t.Context(), node))
	pod.Labels["controller-revision-hash"] = "old-revision"
	require.NoError(t, r.Update(t.Context(), pod))
	f := &metricFixture{r: r, registry: prometheus.NewRegistry(), clock: clocktesting.NewFakeClock(time.Unix(1_800_000_000, 0)), key: client.ObjectKeyFromObject(sts), podKey: client.ObjectKeyFromObject(pod), nodeKey: client.ObjectKeyFromObject(node),
		nodes: []slurmapi.Node{{Name: pod.Name, States: nodeStates(api.V0044NodeStateREBOOTREQUESTED), Reason: &slurmapi.NodeReason{Reason: defaultRebootReason}}}}
	r.metrics, r.clock = newRolloutMetrics(f.registry), f.clock
	slurm.On("ListNodes", mock.Anything).Return(func(context.Context) ([]slurmapi.Node, error) { return f.nodes, f.listErr }).Maybe()
	slurm.On("RebootNodes", mock.Anything, mock.Anything).Return(nil).Maybe()
	slurm.On("UndrainNodes", mock.Anything, mock.Anything).Return(nil).Maybe()
	return f
}

func (f *metricFixture) run(t *testing.T) ctrl.Result {
	t.Helper()
	result, err := f.r.Reconcile(t.Context(), ctrl.Request{NamespacedName: f.key})
	require.NoError(t, err, "metrics must preserve the periodic nil-error contract")
	return result
}

func (f *metricFixture) sts(t *testing.T) *kruisev1b1.StatefulSet {
	t.Helper()
	sts := &kruisev1b1.StatefulSet{}
	require.NoError(t, f.r.Get(t.Context(), f.key, sts))
	return sts
}

func (f *metricFixture) pod(t *testing.T, mutate func(*corev1.Pod)) {
	t.Helper()
	pod := &corev1.Pod{}
	require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
	mutate(pod)
	status := pod.Status.DeepCopy()
	require.NoError(t, f.r.Update(t.Context(), pod))
	pod.Status = *status
	require.NoError(t, f.r.Status().Update(t.Context(), pod))
}

func metricValue(gauge *prometheus.GaugeVec, extra ...string) float64 {
	return gaugeValue(gauge.WithLabelValues(append([]string{"default", "cluster", "workers"}, extra...)...))
}

func gaugeValue(gauge prometheus.Gauge) float64 {
	var metric dto.Metric
	if err := gauge.Write(&metric); err != nil {
		panic(err)
	}
	return metric.GetGauge().GetValue()
}

func TestRolloutMetricsReportWaitReasonsAndErrors(t *testing.T) {
	for _, reason := range rolloutWaitReasonLabelValues {
		t.Run(reason, func(t *testing.T) {
			f := newMetricFixture(t)
			switch reason {
			case waitBudget:
				f.nodes[0].States = nodeStates(api.V0044NodeStateIDLE)
				sts := f.sts(t)
				sts.Status.ReadyReplicas = 0
				require.NoError(t, f.r.Update(t.Context(), sts))
			case waitHandoff:
				f.nodes[0].States = nodeStates(api.V0044NodeStateREBOOTISSUED)
			case waitPods:
				f.nodes[0].States, f.nodes[0].Reason = nodeStates(api.V0044NodeStateIDLE), nil
				f.pod(t, func(p *corev1.Pod) {
					p.Labels["controller-revision-hash"] = "current-revision"
					p.Status.Conditions[0].Status = corev1.ConditionFalse
				})
			case waitSlurm:
				f.listErr = errors.New("Slurm unavailable")
			case waitError:
				f.nodes[0].States = nodeStates(api.V0044NodeStateIDLE)
				f.r.Client = interceptor.NewClient(f.r.Client.(client.WithWatch), interceptor.Funcs{Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
					return errors.New("handoff patch failed")
				}})
			case waitEviction:
				node := &corev1.Node{}
				require.NoError(t, f.r.Get(t.Context(), f.nodeKey, node))
				node.Spec.Unschedulable = true
				require.NoError(t, f.r.Update(t.Context(), node))
				f.pod(t, func(p *corev1.Pod) {
					p.Labels["controller-revision-hash"] = "current-revision"
					p.Labels[consts.LabelSoperatorWorkerOperationID] = "operation"
					p.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseReady
				})
			case waitSafety:
				f.nodes[0].States = nodeStates(api.V0044NodeStateALLOCATED)
				f.pod(t, func(p *corev1.Pod) {
					p.Status.ContainerStatuses = []corev1.ContainerStatus{crashLoopingContainerStatus(consts.ContainerNameSlurmd)}
				})
			case waitMissing:
				f.nodes = nil
			case waitCleanup:
				f.pod(t, func(p *corev1.Pod) { p.Labels["controller-revision-hash"] = "current-revision" })
				f.nodes[0] = staleRollingUpdateNode(f.podKey.Name)
			}
			require.Equal(t, testRollingUpdateInterval, f.run(t).RequeueAfter)
			for _, candidate := range rolloutWaitReasonLabelValues {
				expected := 0.0
				if candidate == reason {
					expected = 1
				}
				require.Equal(t, expected, metricValue(f.r.metrics.waiting, candidate))
			}
			require.Equal(t, 1.0, metricValue(f.r.metrics.active))
			stage := map[string]string{
				waitBudget: "waiting_for_slot", waitReboot: "waiting_for_slurm", waitHandoff: "stopping_worker",
				waitPods: "starting_pod", waitSlurm: "unknown", waitError: "blocked", waitEviction: "waiting_for_eviction",
				waitSafety: "blocked", waitMissing: "missing_slurm_node", waitCleanup: "restoring_slurm",
			}[reason]
			requireWorkerCounts(t, f, map[string]int{stage: 1})
			if reason == waitSlurm || reason == waitError || reason == waitBudget {
				require.Zero(t, metricValue(f.r.metrics.slots))
			}
			if reason == waitEviction {
				require.Zero(t, metricValue(f.r.metrics.outdated), "current-revision cordon handoff remains active until eviction")
			}
		})
	}
}

func TestRolloutMetricsProgressAndCompletion(t *testing.T) {
	f := newMetricFixture(t)
	f.run(t)
	started := metricValue(f.r.metrics.progress)
	f.clock.Step(time.Hour)
	f.run(t)
	require.Equal(t, started, metricValue(f.r.metrics.progress), "polling is not progress")
	f.pod(t, func(p *corev1.Pod) { p.Labels["controller-revision-hash"] = "current-revision" })
	f.nodes[0] = staleRollingUpdateNode(f.podKey.Name)
	f.run(t)
	require.Equal(t, 1.0, metricValue(f.r.metrics.active), "ready pods still need Slurm cleanup")
	f.clock.Step(time.Hour)
	f.listErr = errors.New("cleanup observation failed")
	f.run(t)
	require.Equal(t, 1.0, metricValue(f.r.metrics.active))
	require.Equal(t, 1.0, metricValue(f.r.metrics.waiting, waitSlurm))
	f.listErr = nil
	f.nodes[0] = slurmapi.Node{Name: f.podKey.Name, States: nodeStates(api.V0044NodeStateIDLE)}
	f.run(t)
	require.Zero(t, metricValue(f.r.metrics.active))
	require.Equal(t, 1.0, metricValue(f.r.metrics.slots), "completion preserves the available budget")
	completed := metricValue(f.r.metrics.progress)
	f.run(t)
	require.Equal(t, 1.0, metricValue(f.r.metrics.slots), "idle polling preserves the available budget")
	f.clock.Step(24 * time.Hour)
	f.run(t)
	require.Equal(t, 1.0, metricValue(f.r.metrics.slots), "idle Slurm audits preserve the available budget")
	require.Equal(t, completed, metricValue(f.r.metrics.progress))
	f.r.Client = interceptor.NewClient(f.r.Client.(client.WithWatch), interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
		return errors.New("API unavailable")
	}})
	require.Equal(t, testRollingUpdateInterval, f.run(t).RequeueAfter)
	require.Zero(t, metricValue(f.r.metrics.active), "idle read errors must not reactivate a historical rollout")
	require.Zero(t, metricValue(f.r.metrics.slots), "read errors must not advertise stale available capacity")
	require.Equal(t, completed, metricValue(f.r.metrics.progress))
	require.Equal(t, 1.0, metricValue(f.r.metrics.waiting, waitError))
}

func TestRolloutMetricsResetIdentityAndCleanSeries(t *testing.T) {
	f := newMetricFixture(t)
	f.run(t)
	f.clock.Step(time.Hour)
	sts := f.sts(t)
	require.NoError(t, f.r.Delete(t.Context(), sts))
	sts.UID, sts.ResourceVersion = "new-sts-uid", ""
	require.NoError(t, f.r.Create(t.Context(), sts))
	f.run(t)
	require.Equal(t, float64(f.clock.Now().Unix()), metricValue(f.r.metrics.progress), "recreated object must start a new observation")
	f.clock.Step(time.Hour)
	sts = f.sts(t)
	sts.Labels[consts.LabelNodeSetKey] = "renamed"
	require.NoError(t, f.r.Update(t.Context(), sts))
	f.run(t)
	require.Equal(t, float64(f.clock.Now().Unix()), gaugeValue(f.r.metrics.progress.WithLabelValues("default", "cluster", "renamed")))
	families, err := f.registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		for _, metric := range family.Metric {
			labels := make(prometheus.Labels, len(metric.Label))
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
				if label.GetName() == "nodeset" {
					require.Equal(t, "renamed", label.GetValue())
				}
				require.Contains(t, []string{"controller", "resource_namespace", "slurm_cluster", "nodeset", "reason", "stage"}, label.GetName())
			}
			require.Equal(t, "rollingupdate", labels["controller"])
		}
	}
	f.clock.Step(time.Hour)
	f.registry = prometheus.NewRegistry()
	f.r.metrics = newRolloutMetrics(f.registry)
	f.run(t)
	require.Equal(t, float64(f.clock.Now().Unix()), gaugeValue(f.r.metrics.progress.WithLabelValues("default", "cluster", "renamed")), "new process state starts its own progress clock")
	sts = f.sts(t)
	old := sts.DeepCopy()
	sts.Spec.UpdateStrategy.Type = appsv1.RollingUpdateStatefulSetStrategyType
	require.True(t, rollingUpdateLoopStartPredicate().Update(event.UpdateEvent{ObjectOld: old, ObjectNew: sts}))
	require.NoError(t, f.r.Update(t.Context(), sts))
	require.Zero(t, f.run(t).RequeueAfter)
	families, err = f.registry.Gather()
	require.NoError(t, err)
	require.Empty(t, families)
	require.Empty(t, f.r.metrics.objects)
	sts.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
	require.NoError(t, f.r.Update(t.Context(), sts))
	f.run(t)
	require.True(t, rollingUpdateLoopStartPredicate().Delete(event.DeleteEvent{Object: sts}))
	require.False(t, rollingUpdateLoopStartPredicate().Update(event.UpdateEvent{ObjectOld: sts.DeepCopy(), ObjectNew: sts}), "ordinary updates do not drive the periodic loop")
	require.NoError(t, f.r.Delete(t.Context(), sts))
	require.Zero(t, f.run(t).RequeueAfter)
	require.Empty(t, f.r.metrics.objects)
}

func TestRolloutProgressIgnoresFlapsButTracksReplacementIncarnations(t *testing.T) {
	state := rolloutState{Snapshot: rolloutSnapshot{Incarnations: sha256.Sum256([]byte("first-pod")), Ready: 1, Updated: 1, Replacing: 1}, Progress: rolloutSnapshot{Ready: 1, Updated: 1, Replacing: 1}}
	snapshot := state.Snapshot
	snapshot.Ready = 0
	require.False(t, state.observeProgress(snapshot))
	snapshot.Ready = 1
	require.False(t, state.observeProgress(snapshot))
	snapshot.Incarnations = sha256.Sum256([]byte("replacement-pod"))
	require.True(t, state.observeProgress(snapshot))
	sts := testStatefulSet()
	sts.Spec.Replicas = ptr.To(int32(0))
	require.True(t, rolloutReady(sts, rolloutSnapshot{}))
	require.False(t, rolloutReady(sts, rolloutSnapshot{Replacing: 1}))
	require.False(t, rolloutReady(sts, rolloutSnapshot{Cleanup: 1}))
}

func TestRolloutMetricsKeepSlotsForReleasedCordonHandoff(t *testing.T) {
	f := newMetricFixture(t)
	sts := f.sts(t)
	sts.Spec.Replicas, sts.Status.ReadyReplicas = ptr.To(int32(3)), 3
	setMaxUnavailable(sts, intstr.FromInt32(3))
	pod := &corev1.Pod{}
	require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
	pod.Labels[consts.LabelSoperatorWorkerOperationID], pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = "operation", consts.LabelSoperatorWorkerOperationPhaseReady
	observation := &rolloutObservation{}
	ctx := context.WithValue(t.Context(), rolloutObservationKey{}, observation)
	require.NoError(t, f.r.processWorkerReplacements(ctx, "cluster", sts, []workerReplacement{{pod: *pod, operationID: "operation", k8sNodeCordoned: true}}, []corev1.Pod{*pod}))
	require.Equal(t, 2, observation.slots, "released worker consumes one slot while the drainer has not evicted it")
	require.Equal(t, waitEviction, observation.wait)
}
