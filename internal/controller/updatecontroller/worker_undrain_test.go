package updatecontroller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func TestReconcileBatchUndrainPreservesBudgetAndOtherHandoffs(t *testing.T) {
	tests := []struct {
		name            string
		undrainErr      error
		unreadyDrain    bool
		maxUnavailable  int32
		expectedReboots string
	}{
		{name: "success", maxUnavailable: 3, expectedReboots: "worker-2"},
		{name: "error", undrainErr: assert.AnError, maxUnavailable: 3, expectedReboots: "worker-2"},
		{name: "timeout", undrainErr: context.DeadlineExceeded, maxUnavailable: 3, expectedReboots: "worker-2"},
		{name: "error does not free slots", undrainErr: assert.AnError, maxUnavailable: 2},
		{name: "unready drain is counted once", undrainErr: assert.AnError, unreadyDrain: true, maxUnavailable: 3, expectedReboots: "worker-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			slurmClient := &slurmapifake.MockClient{}
			r, sts, firstPod, _ := testK8sNodeRolloutReconciler(t, slurmClient)
			sts.Spec.Replicas = ptr.To(int32(4))
			sts.Status.ReadyReplicas = 4
			setMaxUnavailable(sts, intstr.FromInt32(tt.maxUnavailable))
			require.NoError(t, r.Update(ctx, sts))
			pods := []*corev1.Pod{firstPod}
			for i := 1; i < 4; i++ {
				pod := firstPod.DeepCopy()
				pod.Name = fmt.Sprintf("worker-%d", i)
				pod.UID = types.UID(pod.Name + "-uid")
				pod.ResourceVersion = ""
				if i == 1 && tt.unreadyDrain {
					pod.Status.Conditions[0].Status = corev1.ConditionFalse
				}
				require.NoError(t, r.Create(ctx, pod))
				pods = append(pods, pod)
			}
			slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{
				staleRollingUpdateNode("worker-0"),
				staleRollingUpdateNode("worker-1"),
				{Name: "worker-2", States: nodeStates(api.V0044NodeStateIDLE)},
				{Name: "worker-3", States: nodeStates(api.V0044NodeStateIDLE)},
			}, nil).Once()
			slurmClient.On("UndrainNodes", mock.Anything, []string{"worker-0", "worker-1"}).Return(tt.undrainErr).Once()
			if tt.expectedReboots != "" {
				slurmClient.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
					NodeList: tt.expectedReboots, ASAP: true,
					Reason: defaultRebootReason, PowerAction: consts.SlurmPowerActionWorkerHandoff,
				}).Return(nil).Once()
			}

			result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
			require.NoError(t, err)
			assert.Equal(t, testRollingUpdateInterval, result.RequeueAfter)
			var started []string
			for _, original := range pods {
				pod := &corev1.Pod{}
				require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(original), pod))
				assert.True(t, workerPDBSelectsPod(t, pod))
				assert.Nil(t, pod.DeletionTimestamp)
				if pod.Labels[consts.LabelSoperatorWorkerOperationPhase] == consts.LabelSoperatorWorkerOperationPhaseStopping {
					started = append(started, pod.Name)
				}
			}
			assert.Equal(t, tt.expectedReboots, strings.Join(started, ","))
			slurmClient.AssertExpectations(t)
		})
	}
}

func TestReconcileBatchUndrainReselectsNodesAfterPartialFailure(t *testing.T) {
	ctx := t.Context()
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, node := testK8sNodeRolloutReconciler(t, slurmClient)
	node.Spec.Unschedulable = false
	require.NoError(t, r.Update(ctx, node))
	sts.Spec.Replicas = ptr.To(int32(3))
	sts.Status.ReadyReplicas = 3
	sts.Status.UpdatedReplicas = 3
	require.NoError(t, r.Update(ctx, sts))
	for i := 1; i < 3; i++ {
		other := pod.DeepCopy()
		other.Name = fmt.Sprintf("worker-%d", i)
		other.UID = types.UID(other.Name + "-uid")
		other.ResourceVersion = ""
		require.NoError(t, r.Create(ctx, other))
	}
	nodes := []slurmapi.Node{
		staleRollingUpdateNode("worker-0"),
		staleRollingUpdateNode("worker-1"),
		staleRollingUpdateNode("worker-2"),
		staleRollingUpdateNode("another-nodeset-worker"),
	}
	nodes[2].Reason.Reason = "maintenance"
	slurmClient.On("ListNodes", mock.Anything).Return(nodes, nil).Once()
	slurmClient.On("UndrainNodes", mock.Anything, []string{"worker-0", "worker-1"}).Return(assert.AnError).Once()
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)}
	result, err := r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, testRollingUpdateInterval, result.RequeueAfter)

	// The batch applied to worker-0, which was then drained again for maintenance.
	// Only worker-1 still has the stale rolling-update drain on the next pass.
	nodes[0].Reason.Reason = "maintenance"
	slurmClient.On("ListNodes", mock.Anything).Return(nodes, nil).Once()
	slurmClient.On("UndrainNodes", mock.Anything, []string{"worker-1"}).Return(nil).Once()
	result, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, testRollingUpdateInterval, result.RequeueAfter)

	nodes[1].States = nodeStates(api.V0044NodeStateIDLE)
	slurmClient.On("ListNodes", mock.Anything).Return(nodes, nil).Once()
	result, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, testRollingUpdateInterval, result.RequeueAfter)
	slurmClient.AssertNumberOfCalls(t, "UndrainNodes", 2)
	slurmClient.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
	slurmClient.AssertExpectations(t)
}

func staleRollingUpdateNode(name string) slurmapi.Node {
	return slurmapi.Node{
		Name: name, States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN),
		Reason: &slurmapi.NodeReason{Reason: defaultRebootReason},
	}
}
