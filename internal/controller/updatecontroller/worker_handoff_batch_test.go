package updatecontroller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func TestWorkerHandoffBatchContinuesAfterPatchErrors(t *testing.T) {
	patchErr := errors.New("patch unavailable")
	rebootErr := errors.New("reboot unavailable")
	conflict := apierrors.NewConflict(corev1.Resource("pods"), "worker-1", errors.New("pod changed"))
	tests := []struct {
		name            string
		failedPods      []string
		patchErr        error
		rebootErr       error
		expectedReboots string
	}{
		{name: "conflict", failedPods: []string{"worker-1"}, patchErr: conflict, expectedReboots: "worker-0,worker-2"},
		{name: "other patch error", failedPods: []string{"worker-1"}, patchErr: patchErr, expectedReboots: "worker-0,worker-2"},
		{name: "patch and reboot errors", failedPods: []string{"worker-1"}, patchErr: patchErr, rebootErr: rebootErr, expectedReboots: "worker-0,worker-2"},
		{name: "all patches fail", failedPods: []string{"worker-0", "worker-1", "worker-2"}, patchErr: patchErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const operationID = "new-revision"
			var pods []*corev1.Pod
			var candidates []workerReplacement
			for i := range 4 {
				pod := testOutdatedPod()
				pod.Name = fmt.Sprintf("worker-%d", i)
				pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
				pods = append(pods, &pod)
				candidates = append(candidates, workerReplacement{pod: pod, operationID: operationID})
			}
			slurmClient := &slurmapifake.MockClient{}
			if tt.expectedReboots != "" {
				slurmClient.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
					NodeList: tt.expectedReboots, ASAP: true,
					Reason: defaultRebootReason, PowerAction: consts.SlurmPowerActionWorkerHandoff,
				}).Return(tt.rebootErr).Once()
			}
			r, kubeClient := testRollingUpdateReconcilerWithPods(t, slurmClient, pods...)
			var patched []string
			r.Client = interceptor.NewClient(kubeClient.(client.WithWatch), interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					patched = append(patched, obj.GetName())
					if slices.Contains(tt.failedPods, obj.GetName()) {
						return tt.patchErr
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			})

			err := r.startWorkerHandoffsWithinBudget(t.Context(), slurmClient, candidates, 3)
			require.ErrorIs(t, err, tt.patchErr)
			if tt.rebootErr != nil {
				require.ErrorIs(t, err, tt.rebootErr)
			}
			for _, name := range tt.failedPods {
				assert.ErrorContains(t, err, name)
			}
			assert.Equal(t, []string{"worker-0", "worker-1", "worker-2"}, patched, "failed patches retain slots and are not retried immediately")
			for i, original := range pods {
				pod := &corev1.Pod{}
				require.NoError(t, kubeClient.Get(t.Context(), client.ObjectKeyFromObject(original), pod))
				if i == 3 || slices.Contains(tt.failedPods, pod.Name) {
					assert.Empty(t, pod.Labels[consts.LabelSoperatorWorkerOperationID])
					assert.Empty(t, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
				} else {
					assert.Equal(t, consts.LabelSoperatorWorkerOperationPhaseStopping, workerOperationPhase(pod, operationID))
				}
			}
			if tt.expectedReboots == "" {
				slurmClient.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
			}
			slurmClient.AssertExpectations(t)
		})
	}
}

func TestReconcileWorkerHandoffContinuesAfterPodStatusConflict(t *testing.T) {
	ctx := t.Context()
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, _ := testK8sNodeRolloutReconciler(t, slurmClient)
	sts.Spec.Replicas = ptr.To(int32(2))
	sts.Status.ReadyReplicas = 2
	setMaxUnavailable(sts, intstr.FromInt32(2))
	require.NoError(t, r.Update(ctx, sts))
	other := pod.DeepCopy()
	other.Name = "worker-1"
	other.UID = "other-worker-uid"
	other.ResourceVersion = ""
	require.NoError(t, r.Create(ctx, other))

	var patchAttempts []string
	kubeClient := r.Client
	r.Client = interceptor.NewClient(kubeClient.(client.WithWatch), interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			patchAttempts = append(patchAttempts, obj.GetName())
			if len(patchAttempts) == 1 {
				// Simulate a kubelet status write after the controller's pod snapshot.
				current := &corev1.Pod{}
				if err := c.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
					return err
				}
				current.Status.Conditions[0].Status = corev1.ConditionFalse
				if err := c.Status().Update(ctx, current); err != nil {
					return err
				}
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	})
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{
		{Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE)},
		{Name: other.Name, States: nodeStates(api.V0044NodeStateIDLE)},
	}, nil).Once()
	slurmClient.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
		NodeList: other.Name, ASAP: true,
		Reason: defaultRebootReason, PowerAction: consts.SlurmPowerActionWorkerHandoff,
	}).Return(nil).Once()
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)}
	result, err := r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, testRollingUpdateInterval, result.RequeueAfter)
	assert.Equal(t, []string{pod.Name, other.Name}, patchAttempts)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.False(t, podReady(pod))
	assert.Empty(t, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
	assert.True(t, workerPDBSelectsPod(t, pod))

	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{
		{Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE)},
		{Name: other.Name, States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateREBOOTREQUESTED)},
	}, nil).Once()
	slurmClient.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
		NodeList: pod.Name, ASAP: true,
		Reason: defaultRebootReason, PowerAction: consts.SlurmPowerActionWorkerHandoff,
	}).Return(nil).Once()
	result, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, testRollingUpdateInterval, result.RequeueAfter)
	assert.Equal(t, []string{pod.Name, other.Name, pod.Name}, patchAttempts)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.Equal(t, consts.LabelSoperatorWorkerOperationPhaseStopping, workerOperationPhase(pod, "node-rollout-"+string(pod.UID)))
	assert.True(t, workerPDBSelectsPod(t, pod))
	slurmClient.AssertExpectations(t)
}
