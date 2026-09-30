package updatecontroller

import (
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

func TestReconcileMissingSlurmNodeConsumesOneBudgetSlot(t *testing.T) {
	tests := []struct {
		name            string
		missingPodReady bool
		handoffStarted  bool
		maxUnavailable  int32
		expectedReboots string
	}{
		{
			name: "ready pod reserves a slot", missingPodReady: true,
			maxUnavailable: 2, expectedReboots: "worker-0",
		},
		{
			name:           "unready pod is not counted twice",
			maxUnavailable: 2, expectedReboots: "worker-0",
		},
		{
			name: "existing handoff keeps its slot", missingPodReady: true, handoffStarted: true,
			maxUnavailable: 2, expectedReboots: "worker-0",
		},
		{
			name: "workers before and after the missing node can start", missingPodReady: true,
			maxUnavailable: 3, expectedReboots: "worker-0,worker-2",
		},
		{
			name: "missing node exhausts a budget of one", missingPodReady: true,
			maxUnavailable: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			slurmClient := &slurmapifake.MockClient{}
			r, sts, firstPod, _ := testK8sNodeRolloutReconciler(t, slurmClient)
			sts.Spec.Replicas = ptr.To(int32(3))
			// Keep status ahead of the cache to exercise readiness accounting for the unready pod.
			sts.Status.ReadyReplicas = 3
			setMaxUnavailable(sts, intstr.FromInt32(tt.maxUnavailable))
			require.NoError(t, r.Update(ctx, sts))

			pods := []*corev1.Pod{firstPod}
			for i := 1; i < 3; i++ {
				pod := firstPod.DeepCopy()
				pod.Name = fmt.Sprintf("worker-%d", i)
				pod.UID = types.UID(pod.Name + "-uid")
				pod.ResourceVersion = ""
				if i == 1 {
					if !tt.missingPodReady {
						pod.Status.Conditions[0].Status = corev1.ConditionFalse
					}
					if tt.handoffStarted {
						pod.Labels[consts.LabelSoperatorWorkerOperationID] = "node-rollout-" + string(pod.UID)
						pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseStopping
					}
				}
				require.NoError(t, r.Create(ctx, pod))
				pods = append(pods, pod)
			}
			slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{
				{Name: pods[0].Name, States: nodeStates(api.V0044NodeStateIDLE)},
				{Name: pods[2].Name, States: nodeStates(api.V0044NodeStateIDLE)},
			}, nil).Once()
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
			for i, original := range pods {
				pod := &corev1.Pod{}
				require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(original), pod))
				assert.True(t, workerPDBSelectsPod(t, pod))
				assert.Nil(t, pod.DeletionTimestamp)
				if i == 1 {
					assert.Equal(t, original.Labels, pod.Labels, "missing node must not change the worker operation")
					assert.Equal(t, original.ResourceVersion, pod.ResourceVersion)
					continue
				}
				if pod.Labels[consts.LabelSoperatorWorkerOperationPhase] == consts.LabelSoperatorWorkerOperationPhaseStopping {
					started = append(started, pod.Name)
				}
			}
			assert.Equal(t, tt.expectedReboots, strings.Join(started, ","))
			slurmClient.AssertExpectations(t)
		})
	}
}

func TestReconcileMissingSlurmNodeStartsHandoffWhenItAppears(t *testing.T) {
	ctx := t.Context()
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, _ := testK8sNodeRolloutReconciler(t, slurmClient)
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node(nil), nil).Once()

	result, err := r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, testRollingUpdateInterval, result.RequeueAfter)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.Empty(t, pod.Labels[consts.LabelSoperatorWorkerOperationID])
	assert.Empty(t, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
	assert.True(t, workerPDBSelectsPod(t, pod))
	slurmClient.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)

	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE),
	}}, nil).Once()
	slurmClient.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
		NodeList: pod.Name, ASAP: true,
		Reason: defaultRebootReason, PowerAction: consts.SlurmPowerActionWorkerHandoff,
	}).Return(nil).Once()

	result, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, testRollingUpdateInterval, result.RequeueAfter)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.Equal(t, consts.LabelSoperatorWorkerOperationPhaseStopping, workerOperationPhase(pod, "node-rollout-"+string(pod.UID)))
	assert.True(t, workerPDBSelectsPod(t, pod), "appearing in Slurm still requires a worker acknowledgement")
	slurmClient.AssertExpectations(t)
}
