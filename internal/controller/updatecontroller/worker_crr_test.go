package updatecontroller

import (
	"context"
	"testing"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	kruisev1a1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
)

func TestWorkerCRRPersistsRecoveryBeforeCreateAndReusesRequestOnRetry(t *testing.T) {
	for _, lostResponse := range []bool{false, true} {
		t.Run(map[bool]string{false: "create rejected", true: "create response lost"}[lostResponse], func(t *testing.T) {
			r, pod, node := testWorkerCRRRecovery(t)
			var attempts []string
			kube := r.Client
			r.Client = interceptor.NewClient(kube.(client.WithWatch), interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					request, ok := obj.(*kruisev1a1.ContainerRecreateRequest)
					if !ok {
						return c.Create(ctx, obj, opts...)
					}
					persisted := &corev1.Pod{}
					require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pod), persisted))
					require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseRecovering, persisted.Labels[consts.LabelSoperatorWorkerOperationPhase])
					require.Equal(t, request.Name, persisted.Annotations[consts.AnnotationSoperatorWorkerRecoveryRequest])
					require.Equal(t, "containerd://original", persisted.Annotations[consts.AnnotationSoperatorWorkerRecoveryContainerID])
					attempts = append(attempts, request.Name)
					if len(attempts) == 1 {
						if lostResponse {
							require.NoError(t, c.Create(ctx, obj, opts...))
						}
						return assert.AnError
					}
					return c.Create(ctx, obj, opts...)
				},
			})
			complete, err := r.reconcileWorkerRecovery(t.Context(), pod, node)
			require.ErrorIs(t, err, assert.AnError)
			require.False(t, complete)
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(pod), pod))
			requestName := pod.Annotations[consts.AnnotationSoperatorWorkerRecoveryRequest]
			require.NotEmpty(t, requestName)
			complete, err = r.reconcileWorkerRecovery(t.Context(), pod, node)
			require.NoError(t, err)
			require.False(t, complete)
			requests := &kruisev1a1.ContainerRecreateRequestList{}
			require.NoError(t, r.List(t.Context(), requests, client.InNamespace(pod.Namespace)))
			require.Len(t, requests.Items, 1)
			request := &requests.Items[0]
			require.Equal(t, requestName, request.Name)
			for _, attempted := range attempts {
				require.Equal(t, requestName, attempted)
			}
			require.Equal(t, pod.Name, request.Spec.PodName)
			require.Len(t, request.Spec.Containers, 1)
			require.Equal(t, consts.ContainerNameSlurmd, request.Spec.Containers[0].Name)
			require.Equal(t, ptr.To(int64(300)), request.Spec.ActiveDeadlineSeconds)
			require.Nil(t, request.Spec.TTLSecondsAfterFinished)
			if request.Spec.Strategy != nil {
				require.False(t, request.Spec.Strategy.ForceRecreate)
			}
			require.True(t, metav1.IsControlledBy(request, pod))
		})
	}
}

func TestWorkerCRRAlreadyExistsIsObservedWithoutCreatingAnotherRequest(t *testing.T) {
	r, pod, node := testWorkerCRRRecovery(t)
	creates := 0
	kube := r.Client
	r.Client = interceptor.NewClient(kube.(client.WithWatch), interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*kruisev1a1.ContainerRecreateRequest); !ok {
				return c.Create(ctx, obj, opts...)
			}
			creates++
			require.NoError(t, c.Create(ctx, obj, opts...))
			return apierrors.NewAlreadyExists(schema.GroupResource{Group: kruisev1a1.GroupVersion.Group, Resource: "containerrecreaterequests"}, obj.GetName())
		},
	})
	complete, err := r.reconcileWorkerRecovery(t.Context(), pod, node)
	require.NoError(t, err)
	require.False(t, complete)
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(pod), pod))
	complete, err = r.reconcileWorkerRecovery(t.Context(), pod, node)
	require.NoError(t, err)
	require.False(t, complete)
	require.Equal(t, 1, creates)
}

func TestWorkerCRRRequiresTerminalRequestAndFreshSlurmRegistration(t *testing.T) {
	for _, tt := range []struct {
		name       string
		phase      kruisev1a1.ContainerRecreateRequestPhase
		registered bool
		complete   bool
		failed     bool
		unchanged  bool
	}{
		{name: "pending despite registration", phase: kruisev1a1.ContainerRecreateRequestPending, registered: true},
		{name: "recreating despite registration", phase: kruisev1a1.ContainerRecreateRequestRecreating, registered: true},
		{name: "completed but Slurm reboot remains issued", phase: kruisev1a1.ContainerRecreateRequestCompleted},
		{name: "completed and registered", phase: kruisev1a1.ContainerRecreateRequestCompleted, registered: true, complete: true},
		{name: "completed but original container remains", phase: kruisev1a1.ContainerRecreateRequestCompleted, registered: true, unchanged: true},
		{name: "completed with failed container", phase: kruisev1a1.ContainerRecreateRequestCompleted, registered: true, failed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, pod, node := testWorkerCRRRecovery(t)
			complete, err := r.reconcileWorkerRecovery(t.Context(), pod, node)
			require.NoError(t, err)
			require.False(t, complete)
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(pod), pod))
			request := &kruisev1a1.ContainerRecreateRequest{}
			require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: pod.Namespace, Name: pod.Annotations[consts.AnnotationSoperatorWorkerRecoveryRequest]}, request))
			request.Status.Phase = tt.phase
			if tt.phase == kruisev1a1.ContainerRecreateRequestCompleted {
				request.Status.CompletionTime = ptr.To(metav1.Now())
				request.Status.ContainerRecreateStates = []kruisev1a1.ContainerRecreateRequestContainerRecreateState{{Name: consts.ContainerNameSlurmd, Phase: kruisev1a1.ContainerRecreateRequestSucceeded}}
				if tt.failed {
					request.Status.ContainerRecreateStates[0].Phase = kruisev1a1.ContainerRecreateRequestFailed
					request.Status.ContainerRecreateStates[0].Message = "container restart failed"
				}
			}
			require.NoError(t, r.Status().Update(t.Context(), request))
			if !tt.unchanged {
				pod.Status.ContainerStatuses[0].ContainerID = "containerd://restarted"
			}
			require.NoError(t, r.Status().Update(t.Context(), pod))
			if tt.registered {
				node.States = nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN)
			}
			complete, err = r.reconcileWorkerRecovery(t.Context(), pod, node)
			if tt.failed {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.complete, complete)
			requests := &kruisev1a1.ContainerRecreateRequestList{}
			require.NoError(t, r.List(t.Context(), requests))
			require.Len(t, requests.Items, 1)
		})
	}
}

func TestWorkerCRRDoesNotRecreateAContainerThatChangedBeforeRequestCreation(t *testing.T) {
	r, pod, node := testWorkerCRRRecovery(t)
	kube := r.Client
	r.Client = interceptor.NewClient(kube.(client.WithWatch), interceptor.Funcs{
		Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
			return assert.AnError
		},
	})
	_, err := r.reconcileWorkerRecovery(t.Context(), pod, node)
	require.ErrorIs(t, err, assert.AnError)
	r.Client = kube
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(pod), pod))
	pod.Status.ContainerStatuses[0].ContainerID = "containerd://restarted-externally"
	require.NoError(t, r.Status().Update(t.Context(), pod))
	node.States = nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN)
	complete, err := r.reconcileWorkerRecovery(t.Context(), pod, node)
	require.NoError(t, err)
	require.True(t, complete)
	requests := &kruisev1a1.ContainerRecreateRequestList{}
	require.NoError(t, r.List(t.Context(), requests))
	require.Empty(t, requests.Items)
}

func TestWorkerCRRRefusesUnsafeRebootsAndUnknownAllocations(t *testing.T) {
	for _, tc := range []string{"automatic undrain", "missing next state", "unknown CPU allocations", "unknown memory allocations"} {
		t.Run(tc, func(t *testing.T) {
			r, pod, node := testWorkerCRRRecovery(t)
			switch tc {
			case "automatic undrain":
				node.NextStateAfterReboot = []api.V0044NodeNextStateAfterReboot{api.V0044NodeNextStateAfterRebootUNDRAIN}
			case "missing next state":
				node.NextStateAfterReboot = nil
			case "unknown CPU allocations":
				node.AllocCPUs = nil
			case "unknown memory allocations":
				node.AllocMemoryMB = nil
			}
			complete, err := r.reconcileWorkerRecovery(t.Context(), pod, node)
			require.Error(t, err)
			require.False(t, complete)
			requests := &kruisev1a1.ContainerRecreateRequestList{}
			require.NoError(t, r.List(t.Context(), requests))
			require.Empty(t, requests.Items)
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(pod), pod))
			require.Empty(t, pod.Annotations[consts.AnnotationSoperatorWorkerRecoveryRequest])
		})
	}
}

func TestWorkerCRRPersistenceConflictNeverCreatesRequest(t *testing.T) {
	r, pod, node := testWorkerCRRRecovery(t)
	newer := pod.DeepCopy()
	newer.Annotations = map[string]string{"concurrent-update": "true"}
	require.NoError(t, r.Update(t.Context(), newer))
	complete, err := r.reconcileWorkerRecovery(t.Context(), pod, node)
	require.True(t, apierrors.IsConflict(err), "stale recovery intent must fail before CRR creation: %v", err)
	require.False(t, complete)
	requests := &kruisev1a1.ContainerRecreateRequestList{}
	require.NoError(t, r.List(t.Context(), requests))
	require.Empty(t, requests.Items)
}

func TestWorkerCRRControllerKeepsUnhealthyWorkerProtected(t *testing.T) {
	for _, tt := range []struct {
		name               string
		cordoned           bool
		previouslyReleased bool
		failed             bool
	}{
		{name: "cordon arrives during recovery"},
		{name: "cordon precedes acknowledgement", cordoned: true},
		{name: "health failure after release", cordoned: true, previouslyReleased: true},
		{name: "failed recovery on cordoned host", cordoned: true, failed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, slurm := newTestWorkerCRRFixture(t)
			pod, node := &corev1.Pod{}, &corev1.Node{}
			require.NoError(t, f.r.Get(t.Context(), f.nodeKey, node))
			node.Spec.Unschedulable = tt.cordoned
			require.NoError(t, f.r.Update(t.Context(), node))
			if tt.previouslyReleased {
				f.nodes[0].Reason.Reason = "manual maintenance"
				f.run(t)
				require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
				require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseReady, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
				require.False(t, workerPDBSelectsPod(t, pod))
				f.nodes[0].Reason.Reason = "[node_problem] GPU failure"
			}

			f.run(t)
			require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
			podUID, host := pod.UID, pod.Spec.NodeName
			require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseRecovering, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
			require.True(t, workerPDBSelectsPod(t, pod))
			requireWorkerCounts(t, f, map[string]int{"waiting_for_node_replacement": 1})
			request := &kruisev1a1.ContainerRecreateRequest{}
			require.NoError(t, f.r.Get(t.Context(), client.ObjectKey{Namespace: pod.Namespace, Name: pod.Annotations[consts.AnnotationSoperatorWorkerRecoveryRequest]}, request))
			request.Status.Phase = kruisev1a1.ContainerRecreateRequestPending
			require.NoError(t, f.r.Status().Update(t.Context(), request))

			node.Spec.Unschedulable = true
			require.NoError(t, f.r.Update(t.Context(), node))
			f.nodes[0].States = nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN)
			f.pod(t, func(pod *corev1.Pod) { pod.Status.ContainerStatuses[0].ContainerID = "containerd://restarted" })
			f.run(t)
			require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
			require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseRecovering, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
			require.True(t, workerPDBSelectsPod(t, pod), "registration cannot release a pod while Kruise may still restart its container")
			requireWorkerCounts(t, f, map[string]int{"waiting_for_node_replacement": 1})

			request.Status.Phase = kruisev1a1.ContainerRecreateRequestCompleted
			request.Status.CompletionTime = ptr.To(metav1.Now())
			request.Status.ContainerRecreateStates = []kruisev1a1.ContainerRecreateRequestContainerRecreateState{{Name: consts.ContainerNameSlurmd, Phase: kruisev1a1.ContainerRecreateRequestSucceeded}}
			if tt.failed {
				request.Status.ContainerRecreateStates[0].Phase = kruisev1a1.ContainerRecreateRequestFailed
				request.Status.ContainerRecreateStates[0].Message = "container restart failed"
			}
			require.NoError(t, f.r.Status().Update(t.Context(), request))
			f.run(t)
			require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
			require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseRecovering, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
			require.True(t, workerPDBSelectsPod(t, pod))
			if tt.failed {
				requireWorkerCounts(t, f, map[string]int{"blocked": 1})
			} else {
				requireNodeReplacementWait(t, f)
				f.run(t)
				require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
				require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseRecovering, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
				require.True(t, workerPDBSelectsPod(t, pod), "successful recovery must not release a health-drained worker")
				requireNodeReplacementWait(t, f)

				f.nodes[0].Reason.Reason = "manual maintenance"
				f.run(t)
				require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
				require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseReady, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
				require.False(t, workerPDBSelectsPod(t, pod))
				requireWorkerCounts(t, f, map[string]int{"waiting_for_eviction": 1})
			}
			require.Equal(t, podUID, pod.UID)
			require.Equal(t, host, pod.Spec.NodeName)
			require.Nil(t, pod.DeletionTimestamp)
			slurm.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
			slurm.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
			slurm.AssertNotCalled(t, "SlurmV0044PostNodesWithResponse", mock.Anything, mock.Anything)
		})
	}
}

func TestCordonedReleasedWorkerIsProtectedWhenRecoveryCannotStart(t *testing.T) {
	for _, failure := range []string{"unsafe reboot", "request creation fails"} {
		t.Run(failure, func(t *testing.T) {
			f, slurm := newTestWorkerCRRFixture(t)
			node, pod := &corev1.Node{}, &corev1.Pod{}
			require.NoError(t, f.r.Get(t.Context(), f.nodeKey, node))
			node.Spec.Unschedulable = true
			require.NoError(t, f.r.Update(t.Context(), node))
			f.nodes[0].Reason.Reason = "manual maintenance"
			f.run(t)
			require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
			require.False(t, workerPDBSelectsPod(t, pod))

			f.nodes[0].Reason.Reason = "[hardware_problem] GPU failure"
			switch failure {
			case "unsafe reboot":
				f.nodes[0].NextStateAfterReboot = []api.V0044NodeNextStateAfterReboot{api.V0044NodeNextStateAfterRebootUNDRAIN}
			case "request creation fails":
				f.r.Client = interceptor.NewClient(f.r.Client.(client.WithWatch), interceptor.Funcs{
					Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
						return assert.AnError
					},
				})
			}
			for range 2 {
				f.run(t)
				require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
				require.Equal(t, consts.LabelSoperatorWorkerOperationPhaseRecovering, pod.Labels[consts.LabelSoperatorWorkerOperationPhase])
				require.True(t, workerPDBSelectsPod(t, pod))
				require.Nil(t, pod.DeletionTimestamp)
				requireWorkerCounts(t, f, map[string]int{"blocked": 1})
			}
			requests := &kruisev1a1.ContainerRecreateRequestList{}
			require.NoError(t, f.r.List(t.Context(), requests))
			require.Empty(t, requests.Items)
			slurm.AssertNotCalled(t, "RebootNodes", mock.Anything, mock.Anything)
			slurm.AssertNotCalled(t, "UndrainNodes", mock.Anything, mock.Anything)
		})
	}
}

func admitTestWorkerCRRs(t *testing.T, kube client.WithWatch) client.WithWatch {
	t.Helper()
	return interceptor.NewClient(kube, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if request, ok := obj.(*kruisev1a1.ContainerRecreateRequest); ok {
				pod := &corev1.Pod{}
				require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: request.Namespace, Name: request.Spec.PodName}, pod))
				if request.Labels == nil {
					request.Labels = make(map[string]string)
				}
				request.Labels[kruisev1a1.ContainerRecreateRequestPodUIDKey] = string(pod.UID)
				for i := range request.Spec.Containers {
					for _, status := range pod.Status.ContainerStatuses {
						if request.Spec.Containers[i].Name == status.Name {
							request.Spec.Containers[i].StatusContext = &kruisev1a1.ContainerRecreateRequestContainerContext{ContainerID: status.ContainerID}
						}
					}
				}
			}
			return c.Create(ctx, obj, opts...)
		},
	})
}

func testWorkerCRRRecovery(t *testing.T) (*RollingUpdateReconciler, *corev1.Pod, *slurmapi.Node) {
	t.Helper()
	pod := testOutdatedPod()
	pod.UID = "worker-uid"
	pod.Labels = map[string]string{
		consts.LabelSoperatorWorkerOperationID: "new-revision", consts.LabelSoperatorWorkerOperationPhase: consts.LabelSoperatorWorkerOperationPhaseAcknowledged,
	}
	pod.Spec.NodeName = "node-0"
	pod.Spec.Containers = []corev1.Container{{Name: consts.ContainerNameSlurmd}, {Name: "munge"}}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: consts.ContainerNameSlurmd, ContainerID: "containerd://original", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
	r, _ := testRollingUpdateReconciler(t, &pod, nil)
	require.NoError(t, kruisev1a1.AddToScheme(r.Scheme))
	r.Client = admitTestWorkerCRRs(t, clientfake.NewClientBuilder().WithScheme(r.Scheme).WithObjects(&pod).
		WithStatusSubresource(&corev1.Pod{}, &kruisev1a1.ContainerRecreateRequest{}).Build())
	node := &slurmapi.Node{
		Name: pod.Name, States: nodeStates(api.V0044NodeStateDOWN, api.V0044NodeStateDRAIN, api.V0044NodeStateREBOOTISSUED),
		Reason: &slurmapi.NodeReason{Reason: "[node_problem] GPU failure"}, NextStateAfterReboot: []api.V0044NodeNextStateAfterReboot{api.V0044NodeNextStateAfterRebootINVALID},
		AllocCPUs: ptr.To(int32(0)), AllocMemoryMB: ptr.To(int64(0)),
	}
	return r, &pod, node
}

func newTestWorkerCRRFixture(t *testing.T) (*metricFixture, *slurmapifake.MockClient) {
	t.Helper()
	f, slurm := newUnhealthyDrainFixture(t)
	require.NoError(t, kruisev1a1.AddToScheme(f.r.Scheme))
	sts := f.sts(t)
	pod, node := &corev1.Pod{}, &corev1.Node{}
	require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
	require.NoError(t, f.r.Get(t.Context(), f.nodeKey, node))
	pod.Spec.Containers = []corev1.Container{{Name: consts.ContainerNameSlurmd}, {Name: "munge"}}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: consts.ContainerNameSlurmd, ContainerID: "containerd://original", Ready: true}}
	pod.Labels[consts.LabelSoperatorWorkerOperationID] = "current-revision"
	pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseAcknowledged
	f.r.Client = admitTestWorkerCRRs(t, clientfake.NewClientBuilder().WithScheme(f.r.Scheme).WithObjects(sts, pod, node).
		WithStatusSubresource(&corev1.Pod{}, &kruisev1a1.ContainerRecreateRequest{}).Build())
	f.nodes[0].States = nodeStates(api.V0044NodeStateDOWN, api.V0044NodeStateDRAIN, api.V0044NodeStateREBOOTISSUED)
	f.nodes[0].NextStateAfterReboot = []api.V0044NodeNextStateAfterReboot{api.V0044NodeNextStateAfterRebootINVALID}
	f.nodes[0].AllocCPUs, f.nodes[0].AllocMemoryMB = ptr.To(int32(0)), ptr.To(int64(0))

	return f, slurm
}

func startTestWorkerCRRRecovery(t *testing.T) (*metricFixture, *slurmapifake.MockClient, *kruisev1a1.ContainerRecreateRequest) {
	t.Helper()
	f, slurm := newTestWorkerCRRFixture(t)
	pod := &corev1.Pod{}
	f.run(t)
	require.NoError(t, f.r.Get(t.Context(), f.podKey, pod))
	request := &kruisev1a1.ContainerRecreateRequest{}
	require.NoError(t, f.r.Get(t.Context(), client.ObjectKey{Namespace: pod.Namespace, Name: pod.Annotations[consts.AnnotationSoperatorWorkerRecoveryRequest]}, request))
	return f, slurm, request
}

func completeTestWorkerCRR(t *testing.T, f *metricFixture, request *kruisev1a1.ContainerRecreateRequest) {
	t.Helper()
	request.Status.Phase = kruisev1a1.ContainerRecreateRequestCompleted
	request.Status.CompletionTime = ptr.To(metav1.Now())
	request.Status.ContainerRecreateStates = []kruisev1a1.ContainerRecreateRequestContainerRecreateState{{Name: consts.ContainerNameSlurmd, Phase: kruisev1a1.ContainerRecreateRequestSucceeded}}
	require.NoError(t, f.r.Status().Update(t.Context(), request))
	f.pod(t, func(pod *corev1.Pod) { pod.Status.ContainerStatuses[0].ContainerID = "containerd://restarted" })
	f.nodes[0].States = nodeStates(api.V0044NodeStateIDLE, api.V0044NodeStateDRAIN)
}
