package updatecontroller

import (
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	clocktesting "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func TestWorkerCleanupAuditsIdleNodeSet(t *testing.T) {
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, clock := testIdleWorkerCleanup(t, slurmClient)
	clean := []slurmapi.Node{
		{Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE)},
		staleRollingUpdateNode("another-nodeset-worker"),
	}
	slurmClient.On("ListNodes", mock.Anything).Return(clean, nil).Once()
	reconcileWorkerCleanupTick(t, r, sts)
	clock.Step(testRollingUpdateInterval)
	reconcileWorkerCleanupTick(t, r, sts)
	clock.Step(testIdleSlurmAuditInterval - testRollingUpdateInterval)

	// A failed audit retries on the regular tick, without waiting another idle interval.
	slurmClient.On("ListNodes", mock.Anything).Return(nil, assert.AnError).Once()
	reconcileWorkerCleanupTick(t, r, sts)
	clock.Step(testRollingUpdateInterval)
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{staleRollingUpdateNode(pod.Name)}, nil).Once()
	slurmClient.On("UndrainNodes", mock.Anything, []string{pod.Name}).Return(nil).Once()
	reconcileWorkerCleanupTick(t, r, sts)

	clock.Step(testRollingUpdateInterval)
	slurmClient.On("ListNodes", mock.Anything).Return(clean, nil).Once()
	reconcileWorkerCleanupTick(t, r, sts)
	clock.Step(testRollingUpdateInterval)
	reconcileWorkerCleanupTick(t, r, sts)
	slurmClient.AssertExpectations(t)
}

func TestWorkerCleanupRestoresPendingSlurmState(t *testing.T) {
	tests := []struct {
		name    string
		states  []api.V0044NodeState
		unready bool
		missing bool
	}{
		{name: "down drain", states: []api.V0044NodeState{api.V0044NodeStateDOWN, api.V0044NodeStateDRAIN}},
		{name: "reboot issued", states: []api.V0044NodeState{api.V0044NodeStateDOWN, api.V0044NodeStateREBOOTISSUED}},
		{name: "pod not ready", states: []api.V0044NodeState{api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN}, unready: true},
		{name: "missing node", missing: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slurmClient := &slurmapifake.MockClient{}
			r, sts, pod, clock := testIdleWorkerCleanup(t, slurmClient)
			if tt.unready {
				pod.Status.Conditions[0].Status = corev1.ConditionFalse
				require.NoError(t, r.Status().Update(t.Context(), pod))
			}
			var nodes []slurmapi.Node
			if !tt.missing {
				node := staleRollingUpdateNode(pod.Name)
				node.States = nodeStates(tt.states...)
				nodes = append(nodes, node)
			}
			slurmClient.On("ListNodes", mock.Anything).Return(nodes, nil).Twice()
			reconcileWorkerCleanupTick(t, r, sts)
			clock.Step(testRollingUpdateInterval)
			reconcileWorkerCleanupTick(t, r, sts)
			slurmClient.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)

			if tt.unready {
				pod.Status.Conditions[0].Status = corev1.ConditionTrue
				require.NoError(t, r.Status().Update(t.Context(), pod))
			}
			clock.Step(testRollingUpdateInterval)
			slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{staleRollingUpdateNode(pod.Name)}, nil).Once()
			slurmClient.On("UndrainNodes", mock.Anything, []string{pod.Name}).Return(nil).Once()
			reconcileWorkerCleanupTick(t, r, sts)
			clock.Step(testRollingUpdateInterval)
			slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE)}}, nil).Once()
			reconcileWorkerCleanupTick(t, r, sts)
			clock.Step(testRollingUpdateInterval)
			reconcileWorkerCleanupTick(t, r, sts)
			slurmClient.AssertExpectations(t)
		})
	}
}

func TestWorkerCleanupRecoversAfterRestart(t *testing.T) {
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, clock := testIdleWorkerCleanup(t, slurmClient)
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE)}}, nil).Once()
	reconcileWorkerCleanupTick(t, r, sts)

	r = NewRollingUpdateReconciler(r.Client, r.Scheme, record.NewFakeRecorder(10),
		r.slurmAPIClients, testRollingUpdateInterval, testIdleSlurmAuditInterval)
	r.clock = clock
	clock.Step(testRollingUpdateInterval)
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{staleRollingUpdateNode(pod.Name)}, nil).Once()
	slurmClient.On("UndrainNodes", mock.Anything, []string{pod.Name}).Return(nil).Once()
	reconcileWorkerCleanupTick(t, r, sts)
	slurmClient.AssertExpectations(t)
}

func TestWorkerCleanupFollowsHandoffThroughPodReplacement(t *testing.T) {
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, clock := testIdleWorkerCleanup(t, slurmClient)
	clean := []slurmapi.Node{{Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE)}}
	slurmClient.On("ListNodes", mock.Anything).Return(clean, nil).Once()
	reconcileWorkerCleanupTick(t, r, sts)

	node := &corev1.Node{}
	require.NoError(t, r.Get(t.Context(), types.NamespacedName{Name: pod.Spec.NodeName}, node))
	node.Spec.Unschedulable = true
	require.NoError(t, r.Update(t.Context(), node))
	slurmClient.On("ListNodes", mock.Anything).Return(clean, nil).Once()
	slurmClient.On("RebootNodes", mock.Anything, mock.Anything).Return(nil).Once()
	reconcileWorkerCleanupTick(t, r, sts)
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(pod), pod))
	pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseReady
	require.NoError(t, r.Update(t.Context(), pod))
	reconcileWorkerCleanupTick(t, r, sts)

	require.NoError(t, r.Delete(t.Context(), pod))
	// There can be a pass with no pod before the StatefulSet creates its replacement.
	reconcileWorkerCleanupTick(t, r, sts)
	node.Spec.Unschedulable = false
	require.NoError(t, r.Update(t.Context(), node))
	pod.UID = "replacement-uid"
	pod.ResourceVersion = ""
	delete(pod.Labels, consts.LabelSoperatorWorkerOperationID)
	delete(pod.Labels, consts.LabelSoperatorWorkerOperationPhase)
	require.NoError(t, r.Create(t.Context(), pod))
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{staleRollingUpdateNode(pod.Name)}, nil).Once()
	slurmClient.On("UndrainNodes", mock.Anything, []string{pod.Name}).Return(nil).Once()
	reconcileWorkerCleanupTick(t, r, sts)
	clock.Step(testRollingUpdateInterval)
	slurmClient.On("ListNodes", mock.Anything).Return(clean, nil).Once()
	reconcileWorkerCleanupTick(t, r, sts)
	clock.Step(testRollingUpdateInterval)
	reconcileWorkerCleanupTick(t, r, sts)
	slurmClient.AssertExpectations(t)
}

func TestWorkerCleanupContinuesDuringNodeRollout(t *testing.T) {
	ctx := t.Context()
	slurmClient := &slurmapifake.MockClient{}
	r, sts, drainingPod, _ := testK8sNodeRolloutReconciler(t, slurmClient)
	sts.Spec.Replicas = ptr.To(int32(2))
	sts.Status.ReadyReplicas = 2
	require.NoError(t, r.Update(ctx, sts))
	recoveredPod := drainingPod.DeepCopy()
	recoveredPod.Name = "worker-1"
	recoveredPod.UID = "recovered-worker-uid"
	recoveredPod.ResourceVersion = ""
	recoveredPod.Spec.NodeName = "replacement-node"
	require.NoError(t, r.Create(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: recoveredPod.Spec.NodeName}}))
	require.NoError(t, r.Create(ctx, recoveredPod))
	nodes := []slurmapi.Node{
		{Name: drainingPod.Name, States: nodeStates(api.V0044NodeStateIDLE)},
		staleRollingUpdateNode(recoveredPod.Name),
	}
	slurmClient.On("ListNodes", mock.Anything).Return(nodes, nil).Once()
	slurmClient.On("UndrainNodes", mock.Anything, []string{recoveredPod.Name}).Return(assert.AnError).Once()
	slurmClient.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
		NodeList: drainingPod.Name, ASAP: true, Reason: defaultRebootReason, PowerAction: consts.SlurmPowerActionWorkerHandoff,
	}).Return(nil).Once()
	reconcileWorkerCleanupTick(t, r, sts)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(drainingPod), drainingPod))
	require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseStopping, drainingPod.Labels[consts.LabelSoperatorWorkerOperationPhase])

	// Even if every handoff now awaits eviction, retry cleanup without undraining the released worker.
	drainingPod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseReady
	require.NoError(t, r.Update(ctx, drainingPod))
	nodes[0] = staleRollingUpdateNode(drainingPod.Name)
	slurmClient.On("ListNodes", mock.Anything).Return(nodes, nil).Once()
	slurmClient.On("UndrainNodes", mock.Anything, []string{recoveredPod.Name}).Return(nil).Once()
	reconcileWorkerCleanupTick(t, r, sts)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(drainingPod), drainingPod))
	assert.Nil(t, drainingPod.DeletionTimestamp)
	assert.Equal(t, consts.LabelSoperatorWorkerOperationPhaseReady, drainingPod.Labels[consts.LabelSoperatorWorkerOperationPhase])
	slurmClient.AssertNumberOfCalls(t, "ListNodes", 2)
	slurmClient.AssertNumberOfCalls(t, "RebootNodes", 1)
	slurmClient.AssertExpectations(t)
}

func TestWorkerCleanupDropsScaledDownWorkers(t *testing.T) {
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, clock := testIdleWorkerCleanup(t, slurmClient)
	sts.Spec.Replicas = ptr.To(int32(2))
	require.NoError(t, r.Update(t.Context(), sts))
	removed := pod.DeepCopy()
	removed.Name = "worker-1"
	removed.UID = "worker-1-uid"
	removed.ResourceVersion = ""
	removed.Status.Conditions[0].Status = corev1.ConditionFalse
	require.NoError(t, r.Create(t.Context(), removed))
	nodes := []slurmapi.Node{
		{Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE)},
		staleRollingUpdateNode(removed.Name),
	}
	slurmClient.On("ListNodes", mock.Anything).Return(nodes, nil).Twice()
	reconcileWorkerCleanupTick(t, r, sts)
	require.NoError(t, r.Delete(t.Context(), removed))
	sts.Spec.Replicas = ptr.To(int32(1))
	require.NoError(t, r.Update(t.Context(), sts))
	clock.Step(testRollingUpdateInterval)
	reconcileWorkerCleanupTick(t, r, sts)
	clock.Step(testRollingUpdateInterval)
	reconcileWorkerCleanupTick(t, r, sts)
	slurmClient.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
	slurmClient.AssertExpectations(t)
}

func testIdleWorkerCleanup(t *testing.T, slurmClient slurmapi.Client) (
	*RollingUpdateReconciler, *kruisev1b1.StatefulSet, *corev1.Pod, *clocktesting.FakeClock,
) {
	t.Helper()
	r, sts, pod, node := testK8sNodeRolloutReconciler(t, slurmClient)
	node.Spec.Unschedulable = false
	require.NoError(t, r.Update(t.Context(), node))
	clock := clocktesting.NewFakeClock(time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC))
	r.clock = clock
	return r, sts, pod, clock
}

func reconcileWorkerCleanupTick(t *testing.T, r *RollingUpdateReconciler, sts *kruisev1b1.StatefulSet) {
	t.Helper()
	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	assert.Equal(t, testRollingUpdateInterval, result.RequeueAfter)
}
