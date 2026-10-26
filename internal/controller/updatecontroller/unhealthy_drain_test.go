package updatecontroller

import (
	"fmt"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func TestUnhealthyDrainDefersRollingUpdateAndReportsReplacementWait(t *testing.T) {
	for _, reason := range []string{
		"[node_problem] gpu health check failed",
		"[hardware_problem] test",
		"Kill task failed: task still running",
		"[compute_maintenance] node replacement process",
		"[compute_maintenance] node reboot process",
		"soperator rolling update : [hardware_problem] GPU check failed : reboot issued",
	} {
		t.Run(reason, func(t *testing.T) {
			f, slurm := newUnhealthyDrainFixture(t)
			f.nodes[0].Reason.Reason = reason
			require.Equal(t, testRollingUpdateInterval, f.run(t).RequeueAfter)
			requireNodeReplacementWait(t, f)
			progress := metricValue(f.r.metrics.progress)
			node := &corev1.Node{}
			require.NoError(t, f.r.Get(t.Context(), f.nodeKey, node))
			node.Spec.Unschedulable = true
			require.NoError(t, f.r.Update(t.Context(), node))
			f.clock.Step(time.Minute)
			f.run(t)
			requireNodeReplacementWait(t, f)
			require.Equal(t, progress, metricValue(f.r.metrics.progress), "repeated health observations are not rollout progress")
			pod := &corev1.Pod{}
			require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
			require.Nil(t, pod.DeletionTimestamp)
			require.Empty(t, pod.Labels[consts.LabelSoperatorWorkerOperationID])
			require.Empty(t, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
			slurm.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
			slurm.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
		})
	}
}

func TestUnhealthyDrainDefersCrashLoopRecoveryDeletion(t *testing.T) {
	for _, state := range []api.V0044NodeState{"", api.V0044NodeStateREBOOTREQUESTED, api.V0044NodeStateREBOOTISSUED} {
		name := string(state)
		if name == "" {
			name = "no reboot"
		}
		for _, container := range []string{consts.ContainerNameWorkerInit, consts.ContainerNameSlurmd} {
			t.Run(name+"/"+container, func(t *testing.T) {
				f, slurm := newUnhealthyDrainFixture(t)
				f.nodes[0].States = nodeStates(api.V0044NodeStateDOWN, api.V0044NodeStateDRAIN)
				if state != "" {
					f.nodes[0].States[state] = struct{}{}
				}
				f.nodes[0].AllocCPUs, f.nodes[0].AllocMemoryMB = ptr.To(int32(0)), ptr.To(int64(0))
				f.pod(t, func(pod *corev1.Pod) {
					status := []corev1.ContainerStatus{crashLoopingContainerStatus(container)}
					if container == consts.ContainerNameWorkerInit {
						pod.Status.InitContainerStatuses = status
					} else {
						pod.Status.ContainerStatuses = status
					}
				})
				f.run(t)
				requireNodeReplacementWait(t, f)
				pod := &corev1.Pod{}
				require.NoError(t, f.r.Get(t.Context(), f.podKey, pod), "the health guard must precede crash-loop deletion and unowned reboot handling")
				require.Nil(t, pod.DeletionTimestamp)
				require.Empty(t, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
				slurm.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
				slurm.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
			})
		}
	}
}

func TestUnhealthyDrainLeavesCapacityForHealthyPeers(t *testing.T) {
	for _, ready := range []bool{true, false} {
		t.Run(fmt.Sprintf("unhealthy pod ready=%t", ready), func(t *testing.T) {
			f, slurm := newUnhealthyDrainFixture(t)
			sts := f.sts(t)
			sts.Spec.Replicas, sts.Status.ReadyReplicas = ptr.To(int32(3)), 3
			setMaxUnavailable(sts, intstr.FromInt32(2))
			require.NoError(t, f.r.Update(t.Context(), sts))
			base := &corev1.Pod{}
			require.NoError(t, f.r.Get(t.Context(), f.podKey, base))
			for i := 1; i < 3; i++ {
				pod := base.DeepCopy()
				pod.Name, pod.UID, pod.ResourceVersion = fmt.Sprintf("worker-%d", i), types.UID(fmt.Sprintf("worker-%d-uid", i)), ""
				require.NoError(t, f.r.Create(t.Context(), pod))
				f.nodes = append(f.nodes, slurmapi.Node{Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE)})
			}
			if !ready {
				f.pod(t, func(pod *corev1.Pod) { pod.Status.Conditions[0].Status = corev1.ConditionFalse })
			}
			f.run(t)
			slurm.AssertNumberOfCalls(t, "RebootNodes", 1)
			expectedNodes := "worker-1,worker-2"
			expectedCounts := map[string]int{"waiting_for_node_replacement": 1, "waiting_for_slurm": 2}
			untouchedPods := []string{"worker-0"}
			if !ready {
				expectedNodes = "worker-1"
				expectedCounts["waiting_for_slurm"] = 1
				expectedCounts["waiting_for_slot"] = 1
				untouchedPods = append(untouchedPods, "worker-2")
			}
			slurm.AssertCalled(t, "RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
				NodeList: expectedNodes, PowerAction: consts.SlurmPowerActionWorkerHandoff,
			})
			slurm.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
			requireWorkerCounts(t, f, expectedCounts)
			require.Zero(t, metricValue(f.r.metrics.slots), "only an unready health-drained worker consumes unavailable capacity")
			for _, name := range untouchedPods {
				pod := &corev1.Pod{}
				require.NoError(t, f.r.Get(t.Context(), client.ObjectKey{Namespace: f.podKey.Namespace, Name: name}, pod))
				require.Nil(t, pod.DeletionTimestamp)
				require.Empty(t, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
			}
		})
	}
}

func TestUnhealthyDrainBlocksRetryAfterHandoffPatchWithoutReboot(t *testing.T) {
	f, slurm := newUnhealthyDrainFixture(t)
	f.pod(t, func(pod *corev1.Pod) {
		pod.Labels[consts.LabelSoperatorWorkerOperationID] = "current-revision"
		pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseStopping
	})
	f.run(t)
	requireNodeReplacementWait(t, f)
	pod := &corev1.Pod{}
	require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
	require.Nil(t, pod.DeletionTimestamp)
	require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseStopping, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
	slurm.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
}

func TestUnhealthyDrainKeepsAcceptedHandoffBudgetReserved(t *testing.T) {
	for _, tt := range []struct {
		name  string
		phase string
		state api.V0044NodeState
	}{
		{name: "reboot requested", phase: consts.LabelSoperatorWorkerOperationPhaseStopping, state: api.V0044NodeStateREBOOTREQUESTED},
		{name: "reboot issued", phase: consts.LabelSoperatorWorkerOperationPhaseStopping, state: api.V0044NodeStateREBOOTISSUED},
		{name: "acknowledged", phase: consts.LabelSoperatorWorkerOperationPhaseAcknowledged},
		{name: "released for eviction", phase: consts.LabelSoperatorWorkerOperationPhaseReady},
		{name: "recovering", phase: consts.LabelSoperatorWorkerOperationPhaseRecovering},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, slurm := newUnhealthyDrainFixture(t)
			sts := f.sts(t)
			sts.Spec.Replicas, sts.Status.ReadyReplicas = ptr.To(int32(3)), 3
			setMaxUnavailable(sts, intstr.FromInt32(2))
			require.NoError(t, f.r.Update(t.Context(), sts))
			base := &corev1.Pod{}
			require.NoError(t, f.r.Get(t.Context(), f.podKey, base))
			for i := 1; i < 3; i++ {
				pod := base.DeepCopy()
				pod.Name, pod.UID, pod.ResourceVersion = fmt.Sprintf("worker-%d", i), types.UID(fmt.Sprintf("peer-%d", i)), ""
				require.NoError(t, f.r.Create(t.Context(), pod))
				f.nodes = append(f.nodes, slurmapi.Node{Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE)})
			}
			f.pod(t, func(pod *corev1.Pod) {
				pod.Labels[consts.LabelSoperatorWorkerOperationID] = "current-revision"
				pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = tt.phase
			})
			if tt.state != "" {
				f.nodes[0].States[tt.state] = struct{}{}
			}
			f.run(t)
			slurm.AssertNumberOfCalls(t, "RebootNodes", 1)
			slurm.AssertCalled(t, "RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
				NodeList: "worker-1", PowerAction: consts.SlurmPowerActionWorkerHandoff,
			})
			requireWorkerCounts(t, f, map[string]int{"waiting_for_node_replacement": 1, "waiting_for_slurm": 1, "waiting_for_slot": 1})
			require.Zero(t, metricValue(f.r.metrics.slots))
			pod := &corev1.Pod{}
			require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
			require.Nil(t, pod.DeletionTimestamp)
			wantPhase := tt.phase
			if wantPhase == consts.LabelSoperatorWorkerOperationPhaseReady {
				wantPhase = consts.LabelSoperatorWorkerOperationPhaseAcknowledged
			}
			require.Equal(t, wantPhase, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
			require.True(t, workerPDBSelectsPod(t, pod))
			slurm.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
		})
	}
}

func TestUnhealthyDrainWaitResumesFromFreshSlurmState(t *testing.T) {
	for _, clearDrain := range []bool{true, false} {
		t.Run(fmt.Sprintf("clear drain=%t", clearDrain), func(t *testing.T) {
			f, slurm := newUnhealthyDrainFixture(t)
			f.run(t)
			requireNodeReplacementWait(t, f)
			slurm.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
			if clearDrain {
				f.nodes[0].States = nodeStates(api.V0044NodeStateIDLE)
			} else {
				f.nodes[0].Reason.Reason = "manual maintenance"
			}
			f.run(t)
			slurm.AssertNumberOfCalls(t, "RebootNodes", 1)
			slurm.AssertCalled(t, "RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
				NodeList: f.podKey.Name, PowerAction: consts.SlurmPowerActionWorkerHandoff,
			})
			require.Zero(t, metricValue(f.r.metrics.waiting, "node_replacement_pending"))
			requireWorkerCounts(t, f, map[string]int{"waiting_for_slurm": 1})
			pod := &corev1.Pod{}
			require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
			require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseStopping, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
		})
	}
}

func TestUnhealthyDrainGuardAllowsUserProblemDrains(t *testing.T) {
	for _, reason := range []string{
		"[user_problem] pod_ephemeral_storage full",
		"[user_problem] [hardware_problem]",
	} {
		t.Run(reason, func(t *testing.T) {
			f, slurm := newUnhealthyDrainFixture(t)
			f.nodes[0].Reason.Reason = reason
			f.run(t)
			slurm.AssertCalled(t, "RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
				NodeList: f.podKey.Name, PowerAction: consts.SlurmPowerActionWorkerHandoff,
			})
			require.Zero(t, metricValue(f.r.metrics.waiting, "node_replacement_pending"))
			requireWorkerCounts(t, f, map[string]int{"waiting_for_slurm": 1})
		})
	}
}

func TestUnhealthyDrainKeepsCordonedWorkerProtected(t *testing.T) {
	for _, tt := range []struct {
		name, phase string
		upToDate    bool
	}{
		{name: "pending update"},
		{name: "unchanged template", upToDate: true},
		{name: "handoff stopping", phase: consts.LabelSoperatorWorkerOperationPhaseStopping},
		{name: "handoff acknowledged", phase: consts.LabelSoperatorWorkerOperationPhaseAcknowledged},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, slurm := newUnhealthyDrainFixture(t)
			f.nodes[0].Reason.Reason = "[hardware_problem] test"
			f.pod(t, func(pod *corev1.Pod) {
				if tt.upToDate {
					pod.Labels["controller-revision-hash"] = "current-revision"
				}
				if tt.phase != "" {
					pod.Labels[consts.LabelSoperatorWorkerOperationID] = "current-revision"
					pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = tt.phase
				}
			})
			node := &corev1.Node{}
			require.NoError(t, f.r.Get(t.Context(), f.nodeKey, node))
			node.Spec.Unschedulable = true
			require.NoError(t, f.r.Update(t.Context(), node))
			for range 2 {
				f.run(t)
				require.Equal(t, 1.0, metricValue(f.r.metrics.waiting, "node_replacement_pending"))
				requireWorkerCounts(t, f, map[string]int{"waiting_for_node_replacement": 1})
				pod := &corev1.Pod{}
				require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
				require.Equal(t, tt.phase, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
				require.True(t, workerPDBSelectsPod(t, pod))
				require.Nil(t, pod.DeletionTimestamp)
			}
			slurm.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
			slurm.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
			slurm.AssertNotCalled(t, "SlurmV0044PostNodesWithResponse", mock.Anything, mock.Anything)
		})
	}
}

func TestReleasedWorkerRegainsProtectionWhenHealthDrainAppears(t *testing.T) {
	f, slurm := newUnhealthyDrainFixture(t)
	node, pod := &corev1.Node{}, &corev1.Pod{}
	require.NoError(t, f.r.Get(t.Context(), f.nodeKey, node))
	node.Spec.Unschedulable = true
	require.NoError(t, f.r.Update(t.Context(), node))
	f.pod(t, func(pod *corev1.Pod) {
		pod.Labels[consts.LabelSoperatorWorkerOperationID] = "current-revision"
		pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseAcknowledged
	})
	f.nodes[0].Reason.Reason = "manual maintenance"
	f.nodes[0].States = nodeStates(api.V0044NodeStateDOWN, api.V0044NodeStateDRAIN, api.V0044NodeStateREBOOTISSUED)
	f.run(t)
	require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
	require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseReady, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
	require.False(t, workerPDBSelectsPod(t, pod))

	f.nodes[0].States = nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN)
	f.nodes[0].Reason.Reason = "[hardware_problem] test"
	for range 2 {
		f.run(t)
		requireNodeReplacementWait(t, f)
		require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
		require.Equal(t, "current-revision", pod.Labels[consts.LabelSoperatorWorkerOperationID])
		require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseAcknowledged, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
		require.True(t, workerPDBSelectsPod(t, pod))
		require.Nil(t, pod.DeletionTimestamp)
	}

	f.nodes[0].Reason.Reason = "manual maintenance"
	f.run(t)
	require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
	require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseReady, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
	require.False(t, workerPDBSelectsPod(t, pod))
	require.Nil(t, pod.DeletionTimestamp)
	slurm.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
	slurm.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
	slurm.AssertNotCalled(t, "SlurmV0044PostNodesWithResponse", mock.Anything, mock.Anything)
}

func newUnhealthyDrainFixture(t *testing.T) (*metricFixture, *slurmapifake.MockClient) {
	t.Helper()
	f := newMetricFixture(t)
	f.nodes[0] = slurmapi.Node{
		Name: f.podKey.Name, States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN),
		Reason: &slurmapi.NodeReason{Reason: "[node_problem] gpu health check failed"},
	}
	slurm, ok := f.r.slurmAPIClients.GetClient(client.ObjectKey{Namespace: f.podKey.Namespace, Name: "cluster"})
	require.True(t, ok)
	return f, slurm.(*slurmapifake.MockClient)
}

func requireNodeReplacementWait(t *testing.T, f *metricFixture) {
	t.Helper()
	require.Equal(t, 1.0, metricValue(f.r.metrics.active))
	require.Equal(t, 1.0, metricValue(f.r.metrics.outdated))
	require.Equal(t, 1.0, metricValue(f.r.metrics.waiting, "node_replacement_pending"))
	requireWorkerCounts(t, f, map[string]int{"waiting_for_node_replacement": 1})
}
