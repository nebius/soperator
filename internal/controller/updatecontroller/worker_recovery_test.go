package updatecontroller

import (
	"net/http"
	"testing"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func TestWorkerDeletionRejectsConcurrentRecoveryPhasePatch(t *testing.T) {
	ctx := t.Context()
	pod := testOutdatedPod()
	pod.UID = "worker-uid"
	pod.Labels = map[string]string{
		consts.LabelSoperatorWorkerOperationID:    "new-revision",
		consts.LabelSoperatorWorkerOperationPhase: consts.LabelSoperatorWorkerOperationPhaseReady,
	}
	r, kube := testRollingUpdateReconciler(t, &pod, nil)
	snapshot := &corev1.Pod{}
	require.NoError(t, kube.Get(ctx, client.ObjectKeyFromObject(&pod), snapshot))

	// Another reconciliation changes the operation after this snapshot selected the pod.
	recovering := snapshot.DeepCopy()
	recovering.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseRecovering
	require.NoError(t, kube.Patch(ctx, recovering, client.MergeFrom(snapshot)))
	require.NotEqual(t, snapshot.ResourceVersion, recovering.ResourceVersion)

	err := r.deleteWorkerPod(ctx, snapshot)
	require.True(t, apierrors.IsConflict(err), "stale resource version must reject deletion: %v", err)
	survivor := &corev1.Pod{}
	require.NoError(t, kube.Get(ctx, client.ObjectKeyFromObject(&pod), survivor))
	require.Nil(t, survivor.DeletionTimestamp)
	require.Equal(t, pod.UID, survivor.UID)
	require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseRecovering, survivor.Labels[consts.LabelSoperatorWorkerOperationPhase])
}

func allowTestWorkerDrains(slurmClient slurmapi.Client) {
	if slurm, ok := slurmClient.(*slurmapifake.MockClient); ok {
		for _, call := range slurm.ExpectedCalls {
			if call.Method == "SlurmV0044PostNodesWithResponse" {
				return
			}
		}
		slurm.On("SlurmV0044PostNodesWithResponse", mock.Anything, mock.MatchedBy(func(body api.V0044UpdateNodeMsg) bool {
			return body.Name != nil && len(*body.Name) > 0 && body.Reason != nil && *body.Reason == defaultRebootReason &&
				body.State != nil && len(*body.State) == 1 && (*body.State)[0] == api.V0044UpdateNodeMsgStateDRAIN
		})).Return(successfulDrainResponse(), nil).Maybe()
	}
}

func successfulDrainResponse() *api.SlurmV0044PostNodesResponse {
	return &api.SlurmV0044PostNodesResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK},
		JSON200:      &api.V0044OpenapiResp{},
	}
}

func TestWorkerHandoffDrainsBeforeReboot(t *testing.T) {
	for _, tt := range []struct {
		name          string
		drainErr      error
		drainResponse *api.SlurmV0044PostNodesResponse
	}{
		{name: "drain succeeds"},
		{name: "drain fails", drainErr: assert.AnError},
		{name: "drain HTTP failure", drainResponse: &api.SlurmV0044PostNodesResponse{HTTPResponse: &http.Response{StatusCode: http.StatusServiceUnavailable}}},
		{name: "drain Slurm error", drainResponse: &api.SlurmV0044PostNodesResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusOK},
			JSON200:      &api.V0044OpenapiResp{Errors: ptr.To([]api.V0044OpenapiError{{}})},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pod := testOutdatedPod()
			sts := testStatefulSet()
			sts.Status.ReadyReplicas = 1
			slurm := &slurmapifake.MockClient{}
			slurm.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE)}}, nil).Once()
			response := tt.drainResponse
			if response == nil {
				response = successfulDrainResponse()
			}
			drainFailed := tt.drainErr != nil || tt.drainResponse != nil
			drain := slurm.On("SlurmV0044PostNodesWithResponse", mock.Anything, api.V0044UpdateNodeMsg{
				Name: ptr.To([]string{pod.Name}), State: ptr.To([]api.V0044UpdateNodeMsgState{api.V0044UpdateNodeMsgStateDRAIN}), Reason: ptr.To(defaultRebootReason),
			}).Return(response, tt.drainErr).Once()
			if !drainFailed {
				slurm.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
					NodeList: pod.Name, PowerAction: consts.SlurmPowerActionWorkerHandoff,
				}).NotBefore(drain).Return(nil).Once()
			}
			r, _ := testRollingUpdateReconciler(t, &pod, slurm)
			err := r.processWorkerReplacements(t.Context(), "cluster", sts, []workerReplacement{{pod: pod, operationID: "new-revision"}}, nil)
			if !drainFailed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				if tt.drainErr != nil {
					require.ErrorIs(t, err, tt.drainErr)
				}
				slurm.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
			}
			slurm.AssertExpectations(t)
		})
	}
}

func TestWorkerHandoffRetriesFailedRebootWithoutUndraining(t *testing.T) {
	pod := testOutdatedPod()
	sts := testStatefulSet()
	sts.Status.ReadyReplicas = 1
	slurm := &slurmapifake.MockClient{}
	slurm.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE)}}, nil).Once()
	slurm.On("SlurmV0044PostNodesWithResponse", mock.Anything, mock.Anything).Return(successfulDrainResponse(), nil).Once()
	request := slurmapi.RebootNodesRequest{NodeList: pod.Name, PowerAction: consts.SlurmPowerActionWorkerHandoff}
	slurm.On("RebootNodes", mock.Anything, request).Return(assert.AnError).Once()
	r, kube := testRollingUpdateReconciler(t, &pod, slurm)
	require.ErrorIs(t, r.processWorkerReplacements(t.Context(), "cluster", sts, []workerReplacement{{pod: pod, operationID: "new-revision"}}, nil), assert.AnError)
	require.NoError(t, kube.Get(t.Context(), client.ObjectKeyFromObject(&pod), &pod))
	require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseStopping, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])

	slurm.On("ListNodes", mock.Anything).Return([]slurmapi.Node{staleRollingUpdateNode(pod.Name)}, nil).Once()
	slurm.On("RebootNodes", mock.Anything, request).Return(nil).Once()
	require.NoError(t, r.processWorkerReplacements(t.Context(), "cluster", sts, []workerReplacement{{pod: pod, operationID: "new-revision"}}, nil))
	slurm.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
	slurm.AssertNumberOfCalls(t, "SlurmV0044PostNodesWithResponse", 1)
	slurm.AssertNumberOfCalls(t, "RebootNodes", 2)
	slurm.AssertExpectations(t)
}

func TestRecoveringWorkerWaitsForObservedSlurmRecovery(t *testing.T) {
	for _, tt := range []struct {
		name    string
		states  []api.V0044NodeState
		missing bool
	}{
		{name: "missing from Slurm", missing: true},
		{name: "reboot requested", states: []api.V0044NodeState{api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN, api.V0044NodeStateREBOOTREQUESTED}},
		{name: "reboot issued", states: []api.V0044NodeState{api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN, api.V0044NodeStateREBOOTISSUED}},
		{name: "offline", states: []api.V0044NodeState{api.V0044NodeStateDOWN, api.V0044NodeStateDRAIN}},
		{name: "not responding", states: []api.V0044NodeState{api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN, api.V0044NodeStateNOTRESPONDING}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, slurm, request := startTestWorkerCRRRecovery(t)
			completeTestWorkerCRR(t, f, request)
			f.nodes[0].States = nodeStates(tt.states...)
			f.nodes[0].Reason.Reason = "manual maintenance"
			if tt.missing {
				f.nodes = nil
			}
			f.pod(t, func(pod *corev1.Pod) {
				pod.Status.InitContainerStatuses = []corev1.ContainerStatus{crashLoopingContainerStatus(consts.ContainerNameWorkerInit)}
				pod.Status.ContainerStatuses[0].State = crashLoopingContainerStatus(consts.ContainerNameSlurmd).State
			})
			f.run(t)
			pod := &corev1.Pod{}
			require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
			require.Nil(t, pod.DeletionTimestamp)
			require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseRecovering, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
			slurm.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
			slurm.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
		})
	}
}

func TestRecoveringWorkerStartsFreshHandoffOnlyAfterRecovery(t *testing.T) {
	for _, tt := range []struct {
		name               string
		unhealthy, drained bool
	}{
		{name: "health drain remains", unhealthy: true, drained: true},
		{name: "healthy drained worker", drained: true},
		{name: "healthy undrained worker"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, slurm, request := startTestWorkerCRRRecovery(t)
			completeTestWorkerCRR(t, f, request)
			if !tt.unhealthy {
				f.nodes[0].Reason.Reason = "manual maintenance"
			}
			if !tt.drained {
				f.nodes[0].States = nodeStates(api.V0044NodeStateIDLE)
			}
			f.run(t)
			pod := &corev1.Pod{}
			require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
			require.Nil(t, pod.DeletionTimestamp, "recovery must establish a fresh worker acknowledgement")
			if tt.unhealthy {
				requireNodeReplacementWait(t, f)
				slurm.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
			} else {
				slurm.AssertCalled(t, "RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{NodeList: pod.Name, PowerAction: consts.SlurmPowerActionWorkerHandoff})
				require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseStopping, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
			}
			if tt.drained {
				slurm.AssertNotCalled(t, "SlurmV0044PostNodesWithResponse", mock.Anything, mock.Anything)
			} else {
				slurm.AssertNumberOfCalls(t, "SlurmV0044PostNodesWithResponse", 1)
			}
			slurm.AssertCalled(t, "ListNodes", mock.Anything)
			slurm.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
		})
	}
}

func TestWorkerAcknowledgementRemainsProtectedUntilControllerDecision(t *testing.T) {
	for _, tt := range []struct {
		name      string
		unhealthy bool
	}{
		{name: "healthy update"},
		{name: "unhealthy update", unhealthy: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, slurm := newUnhealthyDrainFixture(t)
			f.pod(t, func(pod *corev1.Pod) {
				pod.Labels[consts.LabelSoperatorWorkerOperationID] = "current-revision"
				pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseAcknowledged
			})
			if !tt.unhealthy {
				f.nodes[0].Reason.Reason = "manual maintenance"
			}
			pod := &corev1.Pod{}
			require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
			require.True(t, workerPDBSelectsPod(t, pod), "the action acknowledgement must not release PDB protection")
			f.run(t)
			err := f.r.Get(t.Context(), f.podKey, pod)
			if tt.unhealthy {
				require.NoError(t, err)
				require.True(t, workerPDBSelectsPod(t, pod))
				requireNodeReplacementWait(t, f)
			} else {
				require.True(t, apierrors.IsNotFound(err))
			}
			slurm.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
		})
	}
}
