package updatecontroller

import (
	"context"
	"fmt"
	"testing"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func TestRollingUpdatePreservesExistingDrainRequest(t *testing.T) {
	tests := []struct {
		name   string
		reason *slurmapi.NodeReason
	}{
		{name: "manual reason", reason: &slurmapi.NodeReason{Reason: "test"}},
		{name: "reboot suffix", reason: &slurmapi.NodeReason{Reason: "test : reboot issued"}},
		{name: "empty reason", reason: &slurmapi.NodeReason{}},
		{name: "missing reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := testOutdatedPod()
			sts := testStatefulSet()
			sts.Status.ReadyReplicas = 1
			slurm := &slurmapifake.MockClient{}
			slurm.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
				Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN), Reason: tt.reason,
			}}, nil).Once()
			slurm.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
				NodeList: pod.Name, PowerAction: consts.SlurmPowerActionWorkerHandoff,
			}).Return(nil).Once()
			r, _ := testRollingUpdateReconciler(t, &pod, slurm)
			require.NoError(t, r.processWorkerReplacements(t.Context(), "cluster", sts, []workerReplacement{{pod: pod, operationID: "new-revision"}}, nil))
			slurm.AssertNotCalled(t, "SlurmV0044PostNodesWithResponse", mock.Anything, mock.Anything)
			slurm.AssertExpectations(t)
		})
	}
}

func TestRollingUpdateMixedDrainBatchesShareBudgetAndContinueAfterErrors(t *testing.T) {
	tests := []struct {
		name          string
		regularErr    error
		drainErr      error
		preservingErr error
	}{
		{name: "success"},
		{name: "drain fails", drainErr: assert.AnError},
		{name: "drain and preserved reboot fail", drainErr: assert.AnError, preservingErr: context.DeadlineExceeded},
		{name: "regular batch fails", regularErr: assert.AnError},
		{name: "preserving batch fails", preservingErr: context.DeadlineExceeded},
		{name: "both batches fail", regularErr: assert.AnError, preservingErr: context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			sts := testStatefulSet()
			sts.Spec.Replicas = ptr.To(int32(4))
			sts.Status.ReadyReplicas = 4
			setMaxUnavailable(sts, intstr.FromInt32(3))
			var pods []*corev1.Pod
			var replacements []workerReplacement
			var nodes []slurmapi.Node
			for i := range 4 {
				pod := testOutdatedPod()
				pod.Name = fmt.Sprintf("worker-%d", i)
				pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
				pods = append(pods, &pod)
				replacements = append(replacements, workerReplacement{pod: pod, operationID: "new-revision"})
				node := slurmapi.Node{Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE)}
				if i%2 == 0 {
					node.States = nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN)
					node.Reason = &slurmapi.NodeReason{Reason: fmt.Sprintf("maintenance-%d", i)}
				}
				nodes = append(nodes, node)
			}
			slurmClient := &slurmapifake.MockClient{}
			slurmClient.On("ListNodes", mock.Anything).Return(nodes, nil).Once()
			slurmClient.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
				NodeList: "worker-0,worker-2", PowerAction: consts.SlurmPowerActionWorkerHandoff,
			}).Return(tt.preservingErr).Once()
			slurmClient.On("SlurmV0044PostNodesWithResponse", mock.Anything, mock.Anything).Return(successfulDrainResponse(), tt.drainErr).Once()
			if tt.drainErr == nil {
				slurmClient.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
					NodeList: "worker-1", PowerAction: consts.SlurmPowerActionWorkerHandoff,
				}).Return(tt.regularErr).Once()
			}
			r, kubeClient := testRollingUpdateReconcilerWithPods(t, slurmClient, pods...)

			err := r.processWorkerReplacements(ctx, "cluster", sts, replacements, nil)
			if tt.regularErr == nil && tt.preservingErr == nil && tt.drainErr == nil {
				require.NoError(t, err)
			} else {
				if tt.drainErr != nil {
					assert.ErrorIs(t, err, tt.drainErr)
				}
				if tt.regularErr != nil {
					assert.ErrorIs(t, err, tt.regularErr)
				}
				if tt.preservingErr != nil {
					assert.ErrorIs(t, err, tt.preservingErr)
				}
			}
			for i, original := range pods {
				pod := &corev1.Pod{}
				require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(original), pod))
				if i < 3 {
					assert.Equal(t, consts.LabelSoperatorWorkerOperationPhaseStopping, workerOperationPhase(pod, "new-revision"))
				} else {
					assert.Empty(t, pod.Labels[consts.LabelSoperatorWorkerOperationPhase], "both batches share the same three-slot budget")
				}
			}
			slurmClient.AssertExpectations(t)
		})
	}
}
