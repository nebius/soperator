package updatecontroller

import (
	"context"
	"errors"
	"testing"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
	"nebius.ai/slurm-operator/internal/values"
)

func TestContainerCrashLoopBackOff(t *testing.T) {
	statuses := []corev1.ContainerStatus{
		{
			Name: "slurmd",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
			},
		},
		{
			Name: "sidecar",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"},
			},
		},
	}

	assert.True(t, containerCrashLoopBackOff(statuses, "slurmd"))
	assert.False(t, containerCrashLoopBackOff(statuses, "sidecar"))
	assert.False(t, containerCrashLoopBackOff(statuses, "missing"))
}

func TestRollingUpdateEnabledRequiresWorkerAndOnDeleteStrategy(t *testing.T) {
	sts := testStatefulSet()
	sts.Labels = map[string]string{
		consts.LabelWorkerKey: consts.LabelWorkerValue,
	}

	assert.True(t, rollingUpdateEnabled(sts))

	sts.Spec.UpdateStrategy = kruisev1b1.StatefulSetUpdateStrategy{
		Type: appsv1.RollingUpdateStatefulSetStrategyType,
	}
	assert.False(t, rollingUpdateEnabled(sts))

	sts.Spec.UpdateStrategy = kruisev1b1.StatefulSetUpdateStrategy{
		Type: appsv1.OnDeleteStatefulSetStrategyType,
	}
	sts.Labels[consts.LabelWorkerKey] = "false"
	assert.False(t, rollingUpdateEnabled(sts))

	delete(sts.Labels, consts.LabelWorkerKey)
	assert.False(t, rollingUpdateEnabled(sts))
}

func TestRebootBudgetUsesStatefulSetScaleStrategy(t *testing.T) {
	tests := []struct {
		name           string
		replicas       int32
		maxUnavailable string
		want           int
	}{
		{name: "percentage", replicas: 10, maxUnavailable: "40%", want: 4},
		{name: "absolute", replicas: 10, maxUnavailable: "3", want: 3},
		{name: "clamped to replicas", replicas: 2, maxUnavailable: "5", want: 2},
		{name: "invalid falls back to one", replicas: 10, maxUnavailable: "invalid", want: 1},
		{name: "zero replicas", replicas: 0, maxUnavailable: "40%", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sts := testStatefulSet()
			sts.Spec.Replicas = ptr.To(tt.replicas)
			setMaxUnavailable(sts, intstr.Parse(tt.maxUnavailable))

			assert.Equal(t, tt.want, rebootBudget(sts))
		})
	}
}

func TestSafeToDeleteOfflineSlurmNode(t *testing.T) {
	tests := []struct {
		name string
		node slurmapi.Node
		want bool
	}{
		{
			name: "down with no allocations",
			node: slurmapi.Node{
				States:        nodeStates(api.V0044NodeStateDOWN),
				AllocCPUs:     ptr.To(int32(0)),
				AllocMemoryMB: ptr.To(int64(0)),
			},
			want: true,
		},
		{
			name: "not responding with no allocations",
			node: slurmapi.Node{
				States:        nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateNOTRESPONDING),
				AllocCPUs:     ptr.To(int32(0)),
				AllocMemoryMB: ptr.To(int64(0)),
			},
			want: true,
		},
		{
			name: "online idle node",
			node: slurmapi.Node{
				States:        nodeStates(api.V0044NodeStateIDLE),
				AllocCPUs:     ptr.To(int32(0)),
				AllocMemoryMB: ptr.To(int64(0)),
			},
		},
		{
			name: "allocated base state with stale zero allocations",
			node: slurmapi.Node{
				States:        nodeStates(api.V0044NodeStateALLOCATED, api.V0044NodeStateNOTRESPONDING),
				AllocCPUs:     ptr.To(int32(0)),
				AllocMemoryMB: ptr.To(int64(0)),
			},
		},
		{
			name: "allocated CPUs",
			node: slurmapi.Node{
				States:        nodeStates(api.V0044NodeStateNOTRESPONDING),
				AllocCPUs:     ptr.To(int32(1)),
				AllocMemoryMB: ptr.To(int64(0)),
			},
		},
		{
			name: "allocated memory",
			node: slurmapi.Node{
				States:        nodeStates(api.V0044NodeStateNOTRESPONDING),
				AllocCPUs:     ptr.To(int32(0)),
				AllocMemoryMB: ptr.To(int64(1)),
			},
		},
		{
			name: "unknown allocations",
			node: slurmapi.Node{
				States: nodeStates(api.V0044NodeStateDOWN),
			},
		},
		{
			name: "completing",
			node: slurmapi.Node{
				States:        nodeStates(api.V0044NodeStateDOWN, api.V0044NodeStateCOMPLETING),
				AllocCPUs:     ptr.To(int32(0)),
				AllocMemoryMB: ptr.To(int64(0)),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, safeToDeleteOfflineSlurmNode(&tt.node))
		})
	}
}

func TestStaleRollingUpdateDrain(t *testing.T) {
	tests := []struct {
		name    string
		node    slurmapi.Node
		isStale bool
	}{
		{
			name: "exact rolling update reason",
			node: slurmapi.Node{
				States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN),
				Reason: &slurmapi.NodeReason{Reason: defaultRebootReason},
			},
			isStale: true,
		},
		{
			name: "slurm reboot suffix",
			node: slurmapi.Node{
				States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN),
				Reason: &slurmapi.NodeReason{Reason: defaultRebootReason + " : reboot issued [root@timestamp]"},
			},
			isStale: true,
		},
		{
			name: "manual drain",
			node: slurmapi.Node{
				States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN),
				Reason: &slurmapi.NodeReason{Reason: "hardware maintenance"},
			},
		},
		{
			name: "reboot still in progress",
			node: slurmapi.Node{
				States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN, api.V0044NodeStateREBOOTISSUED),
				Reason: &slurmapi.NodeReason{Reason: defaultRebootReason},
			},
		},
		{
			name: "node not responding",
			node: slurmapi.Node{
				States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN, api.V0044NodeStateNOTRESPONDING),
				Reason: &slurmapi.NodeReason{Reason: defaultRebootReason},
			},
		},
		{
			name: "jobs still completing",
			node: slurmapi.Node{
				States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN, api.V0044NodeStateCOMPLETING),
				Reason: &slurmapi.NodeReason{Reason: defaultRebootReason},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.isStale, staleRollingUpdateDrain(&tt.node))
		})
	}
}

func TestProcessRollingUpdateDeletesCrashLoopingWorkerInit(t *testing.T) {
	pod := testOutdatedPod()
	pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
		crashLoopingContainerStatus(consts.ContainerNameWorkerInit),
	}

	reconciler, kubeClient := testRollingUpdateReconciler(t, &pod, nil)
	err := reconciler.processWorkerReplacements(context.Background(), "cluster", testStatefulSet(), []workerReplacement{{pod: pod, operationID: "new-revision"}})
	require.NoError(t, err)
	assertPodDeleted(t, kubeClient, &pod)
}

func TestProcessRollingUpdateDeletesPodWithCompletedWorkerHandoff(t *testing.T) {
	pod := testOutdatedPod()
	pod.Labels = map[string]string{
		consts.LabelSoperatorWorkerOperationID:    "new-revision",
		consts.LabelSoperatorWorkerOperationPhase: consts.LabelSoperatorWorkerOperationPhaseReady,
	}

	reconciler, kubeClient := testRollingUpdateReconciler(t, &pod, nil)
	err := reconciler.processWorkerReplacements(context.Background(), "cluster", testStatefulSet(), []workerReplacement{{pod: pod, operationID: "new-revision"}})
	require.NoError(t, err)
	assertPodDeleted(t, kubeClient, &pod)
}

func TestProcessRollingUpdateDeletesSafelyOfflineCrashLoopingSlurmd(t *testing.T) {
	pod := testOutdatedPod()
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		crashLoopingContainerStatus(consts.ContainerNameSlurmd),
	}

	slurmClient := &slurmapifake.MockClient{}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name: pod.Name,
		States: nodeStates(
			api.V0044NodeStateIDLE,
			api.V0044NodeStateNOTRESPONDING,
			api.V0044NodeStateREBOOTREQUESTED,
		),
		AllocCPUs:     ptr.To(int32(0)),
		AllocMemoryMB: ptr.To(int64(0)),
	}}, nil).Once()

	reconciler, kubeClient := testRollingUpdateReconciler(t, &pod, slurmClient)
	err := reconciler.processWorkerReplacements(context.Background(), "cluster", testStatefulSet(), []workerReplacement{{pod: pod, operationID: "new-revision"}})
	require.NoError(t, err)
	assertPodDeleted(t, kubeClient, &pod)
	slurmClient.AssertExpectations(t)
}

func TestProcessRollingUpdateDeletesSafelyOfflineRebootHandoff(t *testing.T) {
	pod := testOutdatedPod()
	pod.Labels = map[string]string{
		consts.LabelSoperatorWorkerOperationID:    "new-revision",
		consts.LabelSoperatorWorkerOperationPhase: consts.LabelSoperatorWorkerOperationPhaseStopping,
	}
	pod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionTrue},
	}

	slurmClient := &slurmapifake.MockClient{}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name: pod.Name,
		States: nodeStates(
			api.V0044NodeStateDOWN,
			api.V0044NodeStateNOTRESPONDING,
			api.V0044NodeStateREBOOTISSUED,
		),
		AllocCPUs:     ptr.To(int32(0)),
		AllocMemoryMB: ptr.To(int64(0)),
	}}, nil).Once()

	reconciler, kubeClient := testRollingUpdateReconciler(t, &pod, slurmClient)
	err := reconciler.processWorkerReplacements(context.Background(), "cluster", testStatefulSet(), []workerReplacement{{pod: pod, operationID: "new-revision"}})
	require.NoError(t, err)
	assertPodDeleted(t, kubeClient, &pod)
	slurmClient.AssertExpectations(t)
}

func TestProcessRollingUpdateContinuesWithinBudgetAfterSafeDelete(t *testing.T) {
	deletingPod := testOutdatedPod()
	deletingPod.Labels = map[string]string{
		consts.LabelSoperatorWorkerOperationID:    "new-revision",
		consts.LabelSoperatorWorkerOperationPhase: consts.LabelSoperatorWorkerOperationPhaseStopping,
	}
	deletingPod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionTrue},
	}
	candidatePod := testOutdatedPod()
	candidatePod.Name = "worker-1"
	waitingPod := testOutdatedPod()
	waitingPod.Name = "worker-2"

	sts := testStatefulSet()
	sts.Spec.Replicas = ptr.To(int32(3))
	sts.Status.ReadyReplicas = 3
	setMaxUnavailable(sts, intstr.FromInt32(2))

	slurmClient := &slurmapifake.MockClient{}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{
		{
			Name: deletingPod.Name,
			States: nodeStates(
				api.V0044NodeStateDOWN,
				api.V0044NodeStateNOTRESPONDING,
				api.V0044NodeStateREBOOTISSUED,
			),
			AllocCPUs:     ptr.To(int32(0)),
			AllocMemoryMB: ptr.To(int64(0)),
		},
		{
			Name:   candidatePod.Name,
			States: nodeStates(api.V0044NodeStateIDLE),
		},
		{
			Name:   waitingPod.Name,
			States: nodeStates(api.V0044NodeStateIDLE),
		},
	}, nil).Once()
	slurmClient.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
		NodeList:    candidatePod.Name,
		ASAP:        true,
		Reason:      defaultRebootReason,
		PowerAction: consts.SlurmPowerActionWorkerHandoff,
	}).Return(nil).Once()

	reconciler, kubeClient := testRollingUpdateReconcilerWithPods(
		t,
		slurmClient,
		&deletingPod,
		&candidatePod,
		&waitingPod,
	)
	err := reconciler.processWorkerReplacements(context.Background(), "cluster", sts, []workerReplacement{{pod: deletingPod, operationID: "new-revision"}, {pod: candidatePod, operationID: "new-revision"}, {pod: waitingPod, operationID: "new-revision"}})
	require.NoError(t, err)
	assertPodDeleted(t, kubeClient, &deletingPod)

	gotCandidate := &corev1.Pod{}
	require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKeyFromObject(&candidatePod), gotCandidate))
	assert.Equal(t,
		consts.LabelSoperatorWorkerOperationPhaseStopping,
		gotCandidate.Labels[consts.LabelSoperatorWorkerOperationPhase],
	)
	gotWaiting := &corev1.Pod{}
	require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKeyFromObject(&waitingPod), gotWaiting))
	assert.Empty(t, gotWaiting.Labels[consts.LabelSoperatorWorkerOperationPhase])
	slurmClient.AssertExpectations(t)
}

func TestProcessRollingUpdateKeepsSafelyOfflineUnmanagedRebootWithoutHandoff(t *testing.T) {
	pod := testOutdatedPod()
	pod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionTrue},
	}

	slurmClient := &slurmapifake.MockClient{}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name: pod.Name,
		States: nodeStates(
			api.V0044NodeStateDOWN,
			api.V0044NodeStateNOTRESPONDING,
			api.V0044NodeStateREBOOTISSUED,
		),
		AllocCPUs:     ptr.To(int32(0)),
		AllocMemoryMB: ptr.To(int64(0)),
	}}, nil).Once()

	reconciler, kubeClient := testRollingUpdateReconciler(t, &pod, slurmClient)
	err := reconciler.processWorkerReplacements(context.Background(), "cluster", testStatefulSet(), []workerReplacement{{pod: pod, operationID: "new-revision"}})
	require.NoError(t, err)

	got := &corev1.Pod{}
	require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKeyFromObject(&pod), got))
	slurmClient.AssertExpectations(t)
}

func TestProcessRollingUpdateDeletesSafelyOfflineManagedRebootWithoutHandoff(t *testing.T) {
	pod := testOutdatedPod()
	pod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionTrue},
	}

	slurmClient := &slurmapifake.MockClient{}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name: pod.Name,
		States: nodeStates(
			api.V0044NodeStateDOWN,
			api.V0044NodeStateNOTRESPONDING,
			api.V0044NodeStateREBOOTISSUED,
		),
		Reason:        &slurmapi.NodeReason{Reason: defaultRebootReason + " : reboot issued [root@timestamp]"},
		AllocCPUs:     ptr.To(int32(0)),
		AllocMemoryMB: ptr.To(int64(0)),
	}}, nil).Once()

	reconciler, kubeClient := testRollingUpdateReconciler(t, &pod, slurmClient)
	err := reconciler.processWorkerReplacements(context.Background(), "cluster", testStatefulSet(), []workerReplacement{{pod: pod, operationID: "new-revision"}})
	require.NoError(t, err)
	assertPodDeleted(t, kubeClient, &pod)
	slurmClient.AssertExpectations(t)
}

func TestProcessRollingUpdateKeepsCrashLoopingSlurmdWithAllocations(t *testing.T) {
	pod := testOutdatedPod()
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		crashLoopingContainerStatus(consts.ContainerNameSlurmd),
	}

	slurmClient := &slurmapifake.MockClient{}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name:          pod.Name,
		States:        nodeStates(api.V0044NodeStateNOTRESPONDING),
		AllocCPUs:     ptr.To(int32(1)),
		AllocMemoryMB: ptr.To(int64(1024)),
	}}, nil).Once()

	reconciler, kubeClient := testRollingUpdateReconciler(t, &pod, slurmClient)
	err := reconciler.processWorkerReplacements(context.Background(), "cluster", testStatefulSet(), []workerReplacement{{pod: pod, operationID: "new-revision"}})
	require.NoError(t, err)

	got := &corev1.Pod{}
	require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKeyFromObject(&pod), got))
	slurmClient.AssertExpectations(t)
}

func TestProcessRollingUpdateStartsRevisionScopedWorkerOperation(t *testing.T) {
	pod := testOutdatedPod()
	pod.Labels = map[string]string{
		consts.LabelSoperatorWorkerOperationID:    "old-revision",
		consts.LabelSoperatorWorkerOperationPhase: consts.LabelSoperatorWorkerOperationPhaseReady,
	}
	sts := testStatefulSet()
	sts.Status.ReadyReplicas = 1

	slurmClient := &slurmapifake.MockClient{}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name:   pod.Name,
		States: nodeStates(api.V0044NodeStateIDLE),
	}}, nil).Once()
	slurmClient.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
		NodeList:    pod.Name,
		ASAP:        true,
		Reason:      defaultRebootReason,
		PowerAction: consts.SlurmPowerActionWorkerHandoff,
	}).Return(nil).Once()

	reconciler, kubeClient := testRollingUpdateReconciler(t, &pod, slurmClient)
	err := reconciler.processWorkerReplacements(context.Background(), "cluster", sts, []workerReplacement{{pod: pod, operationID: "new-revision"}})
	require.NoError(t, err)

	got := &corev1.Pod{}
	require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKeyFromObject(&pod), got))
	assert.Equal(t, "new-revision", got.Labels[consts.LabelSoperatorWorkerOperationID])
	assert.Equal(t,
		consts.LabelSoperatorWorkerOperationPhaseStopping,
		got.Labels[consts.LabelSoperatorWorkerOperationPhase],
	)
	slurmClient.AssertExpectations(t)
}

func TestProcessRollingUpdateFailsWhenPodIsMissingFromSlurmNodeList(t *testing.T) {
	pod := testOutdatedPod()

	slurmClient := &slurmapifake.MockClient{}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{}, nil).Once()

	reconciler, _ := testRollingUpdateReconciler(t, &pod, slurmClient)
	err := reconciler.processWorkerReplacements(context.Background(), "cluster", testStatefulSet(), []workerReplacement{{pod: pod, operationID: "new-revision"}})
	require.EqualError(t, err, "slurm node worker-0 is missing from list nodes response")
	slurmClient.AssertExpectations(t)
}

func TestProcessRollingUpdateUndrainsStaleDrainBeforeReboot(t *testing.T) {
	pod := testOutdatedPod()
	slurmClient := &slurmapifake.MockClient{}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name:   pod.Name,
		States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN),
		Reason: &slurmapi.NodeReason{Reason: defaultRebootReason + " : reboot issued [root@timestamp]"},
	}}, nil).Once()
	slurmClient.On("UndrainNode", mock.Anything, pod.Name).Return(nil).Once()

	reconciler, kubeClient := testRollingUpdateReconciler(t, &pod, slurmClient)
	err := reconciler.processWorkerReplacements(context.Background(), "cluster", testStatefulSet(), []workerReplacement{{pod: pod, operationID: "new-revision"}})
	require.NoError(t, err)

	got := &corev1.Pod{}
	require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKeyFromObject(&pod), got))
	slurmClient.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
	slurmClient.AssertExpectations(t)
}

func TestProcessRollingUpdateContinuesWithinBudgetAfterUndrain(t *testing.T) {
	undrainedPod := testOutdatedPod()
	undrainedPod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionTrue},
	}
	candidatePod := testOutdatedPod()
	candidatePod.Name = "worker-1"

	sts := testStatefulSet()
	sts.Spec.Replicas = ptr.To(int32(2))
	sts.Status.ReadyReplicas = 2
	setMaxUnavailable(sts, intstr.FromInt32(2))

	slurmClient := &slurmapifake.MockClient{}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{
		{
			Name:   undrainedPod.Name,
			States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN),
			Reason: &slurmapi.NodeReason{Reason: defaultRebootReason + " : reboot issued [root@timestamp]"},
		},
		{
			Name:   candidatePod.Name,
			States: nodeStates(api.V0044NodeStateIDLE),
		},
	}, nil).Once()
	slurmClient.On("UndrainNode", mock.Anything, undrainedPod.Name).Return(nil).Once()
	slurmClient.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
		NodeList:    candidatePod.Name,
		ASAP:        true,
		Reason:      defaultRebootReason,
		PowerAction: consts.SlurmPowerActionWorkerHandoff,
	}).Return(nil).Once()

	reconciler, _ := testRollingUpdateReconcilerWithPods(
		t,
		slurmClient,
		&undrainedPod,
		&candidatePod,
	)
	err := reconciler.processWorkerReplacements(context.Background(), "cluster", sts, []workerReplacement{{pod: undrainedPod, operationID: "new-revision"}, {pod: candidatePod, operationID: "new-revision"}})
	require.NoError(t, err)
	slurmClient.AssertExpectations(t)
}

func TestReconcileUndrainsStaleDrainAfterUpdate(t *testing.T) {
	sts := testStatefulSet()
	sts.Labels = map[string]string{
		consts.LabelWorkerKey:   consts.LabelWorkerValue,
		consts.LabelInstanceKey: "cluster",
	}
	sts.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "worker"}}
	sts.Status.UpdateRevision = "new-revision"
	sts.Status.UpdatedReplicas = 1

	pod := testOutdatedPod()
	pod.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(sts, kruisev1b1.GroupVersion.WithKind("StatefulSet"))}
	pod.Labels = map[string]string{
		"app":                      "worker",
		"controller-revision-hash": sts.Status.UpdateRevision,
	}
	pod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionTrue},
	}

	slurmClient := &slurmapifake.MockClient{}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name:   pod.Name,
		States: nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN),
		Reason: &slurmapi.NodeReason{Reason: defaultRebootReason + " : reboot issued [root@timestamp]"},
	}}, nil).Once()
	slurmClient.On("UndrainNode", mock.Anything, pod.Name).Return(nil).Once()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, kruisev1b1.AddToScheme(scheme))
	kubeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(sts, &pod).Build()
	slurmClients := slurmapi.NewClientSet(context.Background())
	slurmClients.AddClient(types.NamespacedName{Namespace: "default", Name: "cluster"}, slurmClient)
	reconciler := NewRollingUpdateReconciler(
		kubeClient,
		scheme,
		record.NewFakeRecorder(1),
		slurmClients,
	)

	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(sts),
	})
	require.NoError(t, err)
	assert.Equal(t, time.Second, result.RequeueAfter)
	slurmClient.AssertExpectations(t)
}

func nodeStates(states ...api.V0044NodeState) map[api.V0044NodeState]struct{} {
	result := make(map[api.V0044NodeState]struct{}, len(states))
	for _, state := range states {
		result[state] = struct{}{}
	}
	return result
}

func testOutdatedPod() corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:            "worker-0",
		Namespace:       "default",
		ResourceVersion: "1",
	}}
}

func crashLoopingContainerStatus(name string) corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name: name,
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
		},
	}
}

func testStatefulSet() *kruisev1b1.StatefulSet {
	sts := &kruisev1b1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "workers", Namespace: "default"},
		Spec: kruisev1b1.StatefulSetSpec{
			Replicas: ptr.To(int32(1)),
			UpdateStrategy: kruisev1b1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
		},
	}
	setMaxUnavailable(sts, intstr.FromInt32(1))
	return sts
}

func setMaxUnavailable(sts *kruisev1b1.StatefulSet, maxUnavailable intstr.IntOrString) {
	sts.Spec.ScaleStrategy = &kruisev1b1.StatefulSetScaleStrategy{
		MaxUnavailable: ptr.To(maxUnavailable),
	}
}

func testRollingUpdateReconciler(
	t *testing.T,
	pod *corev1.Pod,
	slurmClient slurmapi.Client,
) (*RollingUpdateReconciler, client.Client) {
	t.Helper()
	return testRollingUpdateReconcilerWithPods(t, slurmClient, pod)
}

func testRollingUpdateReconcilerWithPods(
	t *testing.T,
	slurmClient slurmapi.Client,
	pods ...*corev1.Pod,
) (*RollingUpdateReconciler, client.Client) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	objects := make([]client.Object, 0, len(pods))
	for _, pod := range pods {
		objects = append(objects, pod.DeepCopy())
	}
	kubeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	slurmClients := slurmapi.NewClientSet(context.Background())
	if slurmClient != nil {
		slurmClients.AddClient(types.NamespacedName{Namespace: "default", Name: "cluster"}, slurmClient)
	}

	return NewRollingUpdateReconciler(
		kubeClient,
		scheme,
		record.NewFakeRecorder(1),
		slurmClients,
	), kubeClient
}

func assertPodDeleted(t *testing.T, kubeClient client.Client, pod *corev1.Pod) {
	t.Helper()
	err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(pod), &corev1.Pod{})
	assert.True(t, apierrors.IsNotFound(err), "expected pod to be deleted, got: %v", err)
}

func testK8sNodeRolloutReconciler(t *testing.T, slurmClient slurmapi.Client) (*RollingUpdateReconciler, *kruisev1b1.StatefulSet, *corev1.Pod, *corev1.Node) {
	t.Helper()
	sts := testStatefulSet()
	sts.UID = "statefulset-uid"
	sts.Labels = map[string]string{
		consts.LabelWorkerKey:   consts.LabelWorkerValue,
		consts.LabelInstanceKey: "cluster",
	}
	sts.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "worker"}}
	sts.Status.UpdateRevision = "current-revision"
	sts.Status.UpdatedReplicas = 1
	sts.Status.ReadyReplicas = 1
	pod := testOutdatedPod()
	pod.UID = "worker-uid"
	pod.Labels = common.RenderMatchLabels(consts.ComponentTypeNodeSet, "cluster")
	pod.Labels[consts.LabelNodeSetKey] = "workers"
	pod.Labels["app"] = "worker"
	pod.Labels["controller-revision-hash"] = sts.Status.UpdateRevision
	pod.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(sts, kruisev1b1.GroupVersion.WithKind("StatefulSet"))}
	pod.Spec.NodeName = "node-0"
	pod.Status.Phase = corev1.PodRunning
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: pod.Spec.NodeName}, Spec: corev1.NodeSpec{Unschedulable: true}}
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, kruisev1b1.AddToScheme(scheme))
	kubeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(sts, &pod, node).
		WithIndex(&corev1.Pod{}, workerPodNodeNameIndex, func(obj client.Object) []string {
			return []string{obj.(*corev1.Pod).Spec.NodeName}
		}).Build()
	clients := slurmapi.NewClientSet(context.Background())
	if slurmClient != nil {
		clients.AddClient(types.NamespacedName{Namespace: sts.Namespace, Name: "cluster"}, slurmClient)
	}
	return NewRollingUpdateReconciler(kubeClient, scheme, record.NewFakeRecorder(10), clients), sts, &pod, node
}

func TestReconcileK8sNodeRolloutWaitsForHandoffBeforeAllowingEviction(t *testing.T) {
	ctx := context.Background()
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, _ := testK8sNodeRolloutReconciler(t, slurmClient)
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)}
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name: pod.Name, States: nodeStates(api.V0044NodeStateALLOCATED),
		AllocCPUs: ptr.To(int32(16)), AllocMemoryMB: ptr.To(int64(1024)),
	}}, nil).Once()
	slurmClient.On("RebootNodes", mock.Anything, slurmapi.RebootNodesRequest{
		NodeList: pod.Name, ASAP: true, Reason: defaultRebootReason, PowerAction: consts.SlurmPowerActionWorkerHandoff,
	}).Return(nil).Once()
	result, err := r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, time.Minute, result.RequeueAfter)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	operationID := "node-rollout-" + string(pod.UID)
	assert.Equal(t, consts.LabelSoperatorWorkerOperationPhaseStopping, workerOperationPhase(pod, operationID))
	assert.True(t, workerPDBSelectsPod(t, pod))

	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name: pod.Name, States: nodeStates(api.V0044NodeStateALLOCATED, api.V0044NodeStateREBOOTREQUESTED),
		AllocCPUs: ptr.To(int32(16)), AllocMemoryMB: ptr.To(int64(1024)),
	}}, nil).Once()
	_, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.True(t, workerPDBSelectsPod(t, pod))

	// This acknowledgement comes from the existing Slurm worker handoff script.
	pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseReady
	require.NoError(t, r.Update(ctx, pod))
	assert.False(t, workerPDBSelectsPod(t, pod), "handoff acknowledgement itself releases PDB protection")
	versionBeforeReconcile := pod.ResourceVersion
	_, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.False(t, workerPDBSelectsPod(t, pod))
	assert.Nil(t, pod.DeletionTimestamp, "the node drainer performs eviction")
	assert.Equal(t, versionBeforeReconcile, pod.ResourceVersion, "ready requires no second label or patch")
	version := pod.ResourceVersion
	_, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.Equal(t, version, pod.ResourceVersion, "waiting for eviction is idempotent")
	slurmClient.AssertExpectations(t)
}

func TestReconcileK8sNodeRolloutPreservesHandoffAndCompletesAfterUncordon(t *testing.T) {
	ctx := context.Background()
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, node := testK8sNodeRolloutReconciler(t, slurmClient)
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)}
	pod.Labels[consts.LabelSoperatorWorkerOperationID] = "previous-revision"
	pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseStopping
	require.NoError(t, r.Update(ctx, pod))
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name: pod.Name, States: nodeStates(api.V0044NodeStateALLOCATED, api.V0044NodeStateREBOOTREQUESTED),
	}}, nil).Once()
	_, err := r.Reconcile(ctx, req)
	require.NoError(t, err)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.Equal(t, "previous-revision", pod.Labels[consts.LabelSoperatorWorkerOperationID])
	assert.True(t, workerPDBSelectsPod(t, pod))

	node.Spec.Unschedulable = false
	require.NoError(t, r.Update(ctx, node))
	pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseReady
	require.NoError(t, r.Update(ctx, pod))
	_, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	assertPodDeleted(t, r.Client, pod)
	slurmClient.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
	slurmClient.AssertExpectations(t)
}

func TestReconcileK8sNodeRolloutReleasedWorkerConsumesSharedUpdateBudget(t *testing.T) {
	ctx := context.Background()
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, _ := testK8sNodeRolloutReconciler(t, slurmClient)
	sts.Spec.Replicas = ptr.To(int32(2))
	sts.Status.ReadyReplicas = 2
	setMaxUnavailable(sts, intstr.FromInt32(1))
	require.NoError(t, r.Update(ctx, sts))
	pod.Labels[consts.LabelSoperatorWorkerOperationID] = "node-rollout-" + string(pod.UID)
	pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseReady
	require.NoError(t, r.Update(ctx, pod))
	other := pod.DeepCopy()
	other.Name = "worker-1"
	other.UID = "other-worker-uid"
	other.ResourceVersion = ""
	other.Spec.NodeName = "other-node"
	require.NoError(t, r.Create(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: other.Spec.NodeName}}))
	delete(other.Labels, consts.LabelSoperatorWorkerOperationID)
	delete(other.Labels, consts.LabelSoperatorWorkerOperationPhase)
	other.Labels["controller-revision-hash"] = "old-revision"
	require.NoError(t, r.Create(ctx, other))
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name: other.Name, States: nodeStates(api.V0044NodeStateIDLE),
	}}, nil).Once()
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(other), other))
	assert.Empty(t, other.Labels[consts.LabelSoperatorWorkerOperationID])
	slurmClient.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
	slurmClient.AssertExpectations(t)
}

func TestReconcileK8sNodeRolloutFailsClosedWhenSlurmIsUnavailable(t *testing.T) {
	ctx := context.Background()
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, _ := testK8sNodeRolloutReconciler(t, slurmClient)
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node(nil), errors.New("unavailable")).Once()
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.ErrorContains(t, err, "unavailable")
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.True(t, workerPDBSelectsPod(t, pod))
	slurmClient.AssertExpectations(t)
}

func TestReconcileK8sNodeRolloutKeepsOfflineWorkerWithAllocationsProtected(t *testing.T) {
	ctx := context.Background()
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, _ := testK8sNodeRolloutReconciler(t, slurmClient)
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{crashLoopingContainerStatus(consts.ContainerNameSlurmd)}
	require.NoError(t, r.Status().Update(ctx, pod))
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name: pod.Name, States: nodeStates(api.V0044NodeStateDOWN),
		AllocCPUs: ptr.To(int32(4)), AllocMemoryMB: ptr.To(int64(1024)),
	}}, nil).Once()
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.True(t, workerPDBSelectsPod(t, pod))
	slurmClient.AssertExpectations(t)
}

func TestReconcileK8sNodeRolloutUnreadyWorkerCanDrainWithExhaustedBudget(t *testing.T) {
	ctx := context.Background()
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, _ := testK8sNodeRolloutReconciler(t, slurmClient)
	pod.Status.Conditions[0].Status = corev1.ConditionFalse
	require.NoError(t, r.Status().Update(ctx, pod))
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name: pod.Name, States: nodeStates(api.V0044NodeStateALLOCATED),
		AllocCPUs: ptr.To(int32(16)), AllocMemoryMB: ptr.To(int64(1024)),
	}}, nil).Once()
	slurmClient.On("RebootNodes", mock.Anything, mock.Anything).Return(nil).Once()
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.Equal(t, consts.LabelSoperatorWorkerOperationPhaseStopping, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
	assert.True(t, workerPDBSelectsPod(t, pod))
	slurmClient.AssertExpectations(t)
}

func TestPlanWorkerPodReplacementsIgnoresUnownedPodsAndFreshReplacement(t *testing.T) {
	_, sts, pod, node := testK8sNodeRolloutReconciler(t, nil)
	unowned := pod.DeepCopy()
	unowned.OwnerReferences = nil
	k8sNodeCordonStates := map[string]bool{node.Name: true}
	replacements := planWorkerPodReplacements(sts, []corev1.Pod{*unowned}, k8sNodeCordonStates)
	assert.Empty(t, replacements)

	k8sNodeCordonStates[node.Name] = false
	pod.UID = "replacement-uid"
	replacements = planWorkerPodReplacements(sts, []corev1.Pod{*pod}, k8sNodeCordonStates)
	assert.Empty(t, replacements)
}

func TestMarkWorkerOperationReadyRejectsStalePodVersion(t *testing.T) {
	ctx := context.Background()
	r, _, pod, _ := testK8sNodeRolloutReconciler(t, nil)
	stale := pod.DeepCopy()
	pod.Labels[consts.LabelSoperatorWorkerOperationID] = "new-operation"
	require.NoError(t, r.Update(ctx, pod))
	err := r.markWorkerOperationReady(ctx, stale, "previous-operation")
	require.True(t, apierrors.IsConflict(err), "expected a stale pod version conflict, got: %v", err)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.True(t, workerPDBSelectsPod(t, pod))
}

func TestReconcileK8sNodeRolloutTerminatingWorkerConsumesOneBudgetSlot(t *testing.T) {
	ctx := context.Background()
	slurmClient := &slurmapifake.MockClient{}
	r, sts, pod, _ := testK8sNodeRolloutReconciler(t, slurmClient)
	sts.Spec.Replicas = ptr.To(int32(2))
	sts.Status.ReadyReplicas = 1
	setMaxUnavailable(sts, intstr.FromInt32(2))
	require.NoError(t, r.Update(ctx, sts))
	terminating := pod.DeepCopy()
	terminating.Name = "terminating-worker"
	terminating.UID = "terminating-uid"
	terminating.ResourceVersion = ""
	terminating.Finalizers = []string{"test.soperator/hold-deletion"}
	require.NoError(t, r.Create(ctx, terminating))
	require.NoError(t, r.Delete(ctx, terminating))
	slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
		Name: pod.Name, States: nodeStates(api.V0044NodeStateIDLE),
	}}, nil).Once()
	slurmClient.On("RebootNodes", mock.Anything, mock.Anything).Return(nil).Once()
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.NoError(t, err)
	require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.Equal(t, consts.LabelSoperatorWorkerOperationPhaseStopping, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
	slurmClient.AssertExpectations(t)
}

func workerPDBSelectsPod(t *testing.T, pod *corev1.Pod) bool {
	t.Helper()
	pdb := worker.RenderPodDisruptionBudget(&values.SlurmNodeSet{
		Name:            "workers",
		ParentalCluster: client.ObjectKey{Namespace: "default", Name: "cluster"},
		StatefulSet:     values.StatefulSet{Name: "workers"},
	})
	selector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
	require.NoError(t, err)
	return selector.Matches(labels.Set(pod.Labels))
}

func TestReconcileK8sNodeRolloutRecoveryUsesWorkerOperationAndSurvivesUncordon(t *testing.T) {
	for _, containerName := range []string{consts.ContainerNameWorkerInit, consts.ContainerNameSlurmd} {
		t.Run(containerName, func(t *testing.T) {
			ctx := context.Background()
			slurmClient := &slurmapifake.MockClient{}
			r, sts, pod, k8sNode := testK8sNodeRolloutReconciler(t, slurmClient)
			if containerName == consts.ContainerNameWorkerInit {
				pod.Status.InitContainerStatuses = []corev1.ContainerStatus{crashLoopingContainerStatus(containerName)}
			} else {
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{crashLoopingContainerStatus(containerName)}
				slurmClient.On("ListNodes", mock.Anything).Return([]slurmapi.Node{{
					Name: pod.Name, States: nodeStates(api.V0044NodeStateDOWN),
					AllocCPUs: ptr.To(int32(0)), AllocMemoryMB: ptr.To(int64(0)),
				}}, nil).Once()
			}
			require.NoError(t, r.Status().Update(ctx, pod))
			req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)}
			_, err := r.Reconcile(ctx, req)
			require.NoError(t, err)
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
			operationID := "node-rollout-" + string(pod.UID)
			assert.Equal(t, operationID, pod.Labels[consts.LabelSoperatorWorkerOperationID])
			assert.Equal(t, consts.LabelSoperatorWorkerOperationPhaseReady, workerOperationPhase(pod, operationID))
			assert.False(t, workerPDBSelectsPod(t, pod))
			assert.Nil(t, pod.DeletionTimestamp)

			version := pod.ResourceVersion
			_, err = r.Reconcile(ctx, req)
			require.NoError(t, err)
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(pod), pod))
			assert.Equal(t, version, pod.ResourceVersion)

			k8sNode.Spec.Unschedulable = false
			require.NoError(t, r.Update(ctx, k8sNode))
			_, err = r.Reconcile(ctx, req)
			require.NoError(t, err)
			assertPodDeleted(t, r.Client, pod)
			slurmClient.AssertExpectations(t)
		})
	}
}

func TestReconcileStopsWhenK8sNodeReadFails(t *testing.T) {
	ctx := context.Background()
	r, sts, pod, _ := testK8sNodeRolloutReconciler(t, nil)
	readErr := errors.New("node cache unavailable")
	kubeClient := clientfake.NewClientBuilder().WithScheme(r.Scheme).WithObjects(sts, pod).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.Node); ok {
					return readErr
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).Build()
	r.Client = kubeClient
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
	require.ErrorIs(t, err, readErr)
	require.NoError(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	assert.Empty(t, pod.Labels[consts.LabelSoperatorWorkerOperationID])
	assert.True(t, workerPDBSelectsPod(t, pod))
}
