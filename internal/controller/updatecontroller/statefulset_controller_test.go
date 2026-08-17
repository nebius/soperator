package updatecontroller

import (
	"context"
	"testing"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
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

func TestSafeToDeleteCrashLoopingSlurmdPod(t *testing.T) {
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
			assert.Equal(t, tt.want, safeToDeleteCrashLoopingSlurmdPod(&tt.node))
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
	slurmClient.On("GetNode", mock.Anything, pod.Name).Return(slurmapi.Node{
		Name: pod.Name,
		States: nodeStates(
			api.V0044NodeStateIDLE,
			api.V0044NodeStateNOTRESPONDING,
			api.V0044NodeStateREBOOTREQUESTED,
		),
		AllocCPUs:     ptr.To(int32(0)),
		AllocMemoryMB: ptr.To(int64(0)),
	}, nil).Once()

	reconciler, kubeClient := testRollingUpdateReconciler(t, &pod, slurmClient)
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
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
	slurmClient.On("GetNode", mock.Anything, pod.Name).Return(slurmapi.Node{
		Name:          pod.Name,
		States:        nodeStates(api.V0044NodeStateNOTRESPONDING),
		AllocCPUs:     ptr.To(int32(1)),
		AllocMemoryMB: ptr.To(int64(1024)),
	}, nil).Once()

	reconciler, kubeClient := testRollingUpdateReconciler(t, &pod, slurmClient)
	err := reconciler.processRollingUpdate(
		context.Background(),
		"cluster",
		testStatefulSet(),
		[]corev1.Pod{pod},
	)
	require.NoError(t, err)

	got := &corev1.Pod{}
	require.NoError(t, kubeClient.Get(context.Background(), client.ObjectKeyFromObject(&pod), got))
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
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker-0", Namespace: "default"}}
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
		Spec:       kruisev1b1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
	}
}

func testRollingUpdateReconciler(
	t *testing.T,
	pod *corev1.Pod,
	slurmClient slurmapi.Client,
) (*RollingUpdateReconciler, client.Client) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	kubeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(pod.DeepCopy()).Build()
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
