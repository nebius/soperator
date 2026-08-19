package updatecontroller

import (
	"context"
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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
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

func TestRollingUpdateEnabledRequiresOnDeleteStrategy(t *testing.T) {
	sts := testStatefulSet()
	sts.Labels = map[string]string{
		consts.LabelSoperatorRollingUpdateEnabled: consts.LabelSoperatorRollingUpdateValue,
	}

	assert.True(t, rollingUpdateEnabled(sts))

	sts.Spec.UpdateStrategy = kruisev1b1.StatefulSetUpdateStrategy{
		Type: appsv1.RollingUpdateStatefulSetStrategyType,
	}
	assert.False(t, rollingUpdateEnabled(sts))

	sts.Spec.UpdateStrategy = kruisev1b1.StatefulSetUpdateStrategy{
		Type: appsv1.OnDeleteStatefulSetStrategyType,
	}
	sts.Labels[consts.LabelSoperatorRollingUpdateEnabled] = "false"
	assert.False(t, rollingUpdateEnabled(sts))
}

func TestRebootBudgetUsesStatefulSetAnnotation(t *testing.T) {
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
			sts.Annotations = map[string]string{
				consts.AnnotationSoperatorRollingUpdateMaxUnavailable: tt.maxUnavailable,
			}

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
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		"new-revision",
		testStatefulSet(),
		[]corev1.Pod{pod},
	)
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
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		"new-revision",
		testStatefulSet(),
		[]corev1.Pod{pod},
	)
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
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		"new-revision",
		testStatefulSet(),
		[]corev1.Pod{pod},
	)
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
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		"new-revision",
		testStatefulSet(),
		[]corev1.Pod{pod},
	)
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
	sts.Annotations = map[string]string{
		consts.AnnotationSoperatorRollingUpdateMaxUnavailable: "2",
	}

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
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		"new-revision",
		sts,
		[]corev1.Pod{deletingPod, candidatePod, waitingPod},
	)
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
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		"new-revision",
		testStatefulSet(),
		[]corev1.Pod{pod},
	)
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
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		"new-revision",
		testStatefulSet(),
		[]corev1.Pod{pod},
	)
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
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		"new-revision",
		testStatefulSet(),
		[]corev1.Pod{pod},
	)
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
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		"new-revision",
		sts,
		[]corev1.Pod{pod},
	)
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
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		"new-revision",
		testStatefulSet(),
		[]corev1.Pod{pod},
	)
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
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		"new-revision",
		testStatefulSet(),
		[]corev1.Pod{pod},
	)
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
	sts.Annotations = map[string]string{
		consts.AnnotationSoperatorRollingUpdateMaxUnavailable: "2",
	}

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
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		"new-revision",
		sts,
		[]corev1.Pod{undrainedPod, candidatePod},
	)
	require.NoError(t, err)
	slurmClient.AssertExpectations(t)
}

func TestReconcileUndrainsStaleDrainAfterUpdate(t *testing.T) {
	sts := testStatefulSet()
	sts.Labels = map[string]string{
		consts.LabelSoperatorRollingUpdateEnabled: consts.LabelSoperatorRollingUpdateValue,
		consts.LabelInstanceKey:                   "cluster",
	}
	sts.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "worker"}}
	sts.Status.UpdateRevision = "new-revision"
	sts.Status.UpdatedReplicas = 1

	pod := testOutdatedPod()
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
	return &kruisev1b1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "workers", Namespace: "default"},
		Spec: kruisev1b1.StatefulSetSpec{
			Replicas: ptr.To(int32(1)),
			UpdateStrategy: kruisev1b1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
		},
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
