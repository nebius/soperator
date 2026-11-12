package nodesetcontroller

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
)

func newStaticRecreateReconciler(
	t *testing.T,
	funcs interceptor.Funcs,
	objects ...client.Object,
) (*NodeSetReconciler, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&slurmv1alpha1.NodeSetPowerState{}).
		WithInterceptorFuncs(funcs).
		Build()
	base := reconciler.NewReconciler(fakeClient, scheme, record.NewFakeRecorder(10))
	return &NodeSetReconciler{
		Reconciler:        base,
		NodeSetPowerState: reconciler.NewNodeSetPowerStateReconciler(base),
	}, fakeClient
}

func newRecreatePowerState(nodeSet *slurmv1alpha1.NodeSet, requests ...slurmv1alpha1.NodeRecreateRequest) *slurmv1alpha1.NodeSetPowerState {
	return &slurmv1alpha1.NodeSetPowerState{
		ObjectMeta: metav1.ObjectMeta{Name: nodeSet.Name, Namespace: nodeSet.Namespace},
		Spec: slurmv1alpha1.NodeSetPowerStateSpec{
			NodeSetRef:    nodeSet.Name,
			ActiveNodes:   []int32{0, 1, 2},
			RecreateNodes: requests,
		},
	}
}

func newRecreateWorkerPod(ordinal int32, slurmdStarted time.Time) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              fmt.Sprintf("worker-%d", ordinal),
			Namespace:         "test-namespace",
			UID:               types.UID(fmt.Sprintf("uid-%d", ordinal)),
			CreationTimestamp: metav1.NewTime(slurmdStarted.Add(-time.Minute)),
			Labels:            map[string]string{consts.LabelNodeSetKey: "worker"},
		},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{
				Name:  consts.ContainerNameMunge,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(slurmdStarted.Add(-time.Hour))}},
			},
			{
				Name:  consts.ContainerNameSlurmd,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(slurmdStarted)}},
			},
		}},
	}
}

func TestReconcileStaticNodeSetPowerState(t *testing.T) {
	recreateRequests := []slurmv1alpha1.NodeRecreateRequest{{
		Ordinal:     2,
		RequestedAt: metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	}}
	tests := []struct {
		name     string
		existing *slurmv1alpha1.NodeSetPowerStateSpec
	}{
		{
			name: "power state is created for a static NodeSet",
		},
		{
			name: "power state left by ephemeral mode is fully activated",
			existing: &slurmv1alpha1.NodeSetPowerStateSpec{
				NodeSetRef:    "test-nodeset",
				ActiveNodes:   []int32{1},
				RecreateNodes: recreateRequests,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeSet := &slurmv1alpha1.NodeSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeset", Namespace: "test-namespace", UID: "test-uid"},
				Spec: slurmv1alpha1.NodeSetSpec{
					EphemeralNodes:              ptr.To(false),
					Replicas:                    3,
					InitialNumberEphemeralNodes: 1,
				},
			}
			objects := []client.Object{nodeSet}
			if tt.existing != nil {
				objects = append(objects, &slurmv1alpha1.NodeSetPowerState{
					ObjectMeta: metav1.ObjectMeta{Name: nodeSet.Name, Namespace: nodeSet.Namespace},
					Spec:       *tt.existing,
				})
			}
			r, fakeClient := newStaticRecreateReconciler(t, interceptor.Funcs{}, objects...)

			// The status follows the activated ordinals on the next reconcile.
			for range 2 {
				require.NoError(t, r.reconcileStaticNodeSetPowerState(context.Background(), nodeSet))
			}

			var powerState slurmv1alpha1.NodeSetPowerState
			require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKeyFromObject(nodeSet), &powerState))
			assert.Equal(t, []int32{0, 1, 2}, powerState.Spec.ActiveNodes)
			assert.Equal(t, int32(3), powerState.Status.ActiveCount)
			if tt.existing != nil {
				assert.Equal(t, recreateRequests[0].Ordinal, powerState.Spec.RecreateNodes[0].Ordinal)
			}
		})
	}
}

func TestRecreateRequestedPods(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	past := now.Add(-time.Hour)
	// The default SuspendTimeout of 90s and the margin put the deadline 100s after the request.
	pastDeadline := past.Add(100 * time.Second)

	tests := []struct {
		name           string
		requestedAt    time.Time
		suspendTimeout *int32
		ordinal        int32
		pod            *corev1.Pod
		deleteErr      error
		wantDeleted    bool
		wantKept       bool
		wantRequeue    time.Duration
		wantErr        bool
	}{
		{
			name:        "pod is kept until SuspendTimeout passes",
			requestedAt: now,
			ordinal:     1,
			pod:         newRecreateWorkerPod(1, past),
			wantKept:    true,
			wantRequeue: 100 * time.Second,
		},
		{
			name:        "pod with slurmd started before the request is deleted",
			requestedAt: past,
			ordinal:     1,
			pod:         newRecreateWorkerPod(1, past.Add(-time.Hour)),
			wantDeleted: true,
		},
		{
			name:        "pod with slurmd started within SuspendTimeout of the request is deleted",
			requestedAt: past,
			ordinal:     1,
			pod:         newRecreateWorkerPod(1, past.Add(30*time.Second)),
			wantDeleted: true,
		},
		{
			name:        "pod with slurmd started after SuspendTimeout already runs a fresh slurmd",
			requestedAt: past,
			ordinal:     1,
			pod:         newRecreateWorkerPod(1, pastDeadline),
		},
		{
			name:        "slurmd restarted after SuspendTimeout in a pod created before the request is kept",
			requestedAt: past,
			ordinal:     1,
			pod: func() *corev1.Pod {
				pod := newRecreateWorkerPod(1, pastDeadline)
				pod.CreationTimestamp = metav1.NewTime(past.Add(-time.Hour))
				return pod
			}(),
		},
		{
			name:        "pod with slurmd not running yet is kept",
			requestedAt: past,
			ordinal:     1,
			pod: func() *corev1.Pod {
				pod := newRecreateWorkerPod(1, past.Add(-time.Hour))
				pod.Status.ContainerStatuses[1].State = corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
				}
				return pod
			}(),
		},
		{
			name:           "SuspendTimeout comes from the cluster",
			requestedAt:    now.Add(-30 * time.Second),
			suspendTimeout: ptr.To(int32(10)),
			ordinal:        1,
			pod:            newRecreateWorkerPod(1, past),
			wantDeleted:    true,
		},
		{
			name:        "terminating pod is left to finish",
			requestedAt: past,
			ordinal:     1,
			pod: func() *corev1.Pod {
				pod := newRecreateWorkerPod(1, past.Add(-time.Hour))
				pod.DeletionTimestamp = ptr.To(metav1.NewTime(past))
				pod.Finalizers = []string{"test/finalizer"}
				return pod
			}(),
		},
		{
			name:        "missing pod is being recreated",
			requestedAt: past,
			ordinal:     1,
		},
		{
			name:        "pod of another NodeSet is not deleted",
			requestedAt: past,
			ordinal:     1,
			pod: func() *corev1.Pod {
				pod := newRecreateWorkerPod(1, past.Add(-time.Hour))
				pod.Labels[consts.LabelNodeSetKey] = "other"
				return pod
			}(),
		},
		{
			name:        "ordinal out of replicas is dropped",
			requestedAt: past,
			ordinal:     5,
			pod:         newRecreateWorkerPod(5, past.Add(-time.Hour)),
		},
		{
			name:        "pod replaced behind a stale cache is checked again",
			requestedAt: past,
			ordinal:     1,
			pod:         newRecreateWorkerPod(1, past.Add(-time.Hour)),
			deleteErr:   apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, "worker-1", errors.New("uid mismatch")),
			wantDeleted: true,
			wantKept:    true,
			wantRequeue: recreateRetryInterval,
		},
		{
			name:        "failed delete keeps the request",
			requestedAt: past,
			ordinal:     1,
			pod:         newRecreateWorkerPod(1, past.Add(-time.Hour)),
			deleteErr:   apierrors.NewServiceUnavailable("try again"),
			wantDeleted: true,
			wantKept:    true,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeSet := &slurmv1alpha1.NodeSet{
				ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test-namespace"},
				Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 3},
			}
			powerState := newRecreatePowerState(nodeSet, slurmv1alpha1.NodeRecreateRequest{
				Ordinal:     tt.ordinal,
				RequestedAt: metav1.NewTime(tt.requestedAt),
			})
			objects := []client.Object{powerState}
			if tt.pod != nil {
				objects = append(objects, tt.pod)
			}

			var deletedUIDs []types.UID
			r, fakeClient := newStaticRecreateReconciler(t, interceptor.Funcs{
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					deleteOptions := &client.DeleteOptions{}
					deleteOptions.ApplyOptions(opts)
					require.NotNil(t, deleteOptions.Preconditions)
					require.NotNil(t, deleteOptions.Preconditions.UID)
					deletedUIDs = append(deletedUIDs, *deleteOptions.Preconditions.UID)
					if tt.deleteErr != nil {
						return tt.deleteErr
					}
					return c.Delete(ctx, obj, opts...)
				},
			}, objects...)
			cluster := &slurmv1.SlurmCluster{}
			cluster.Spec.SlurmConfig.SuspendTimeout = tt.suspendTimeout

			requeueAfter, err := r.recreateRequestedPods(context.Background(), nodeSet, cluster)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.wantRequeue == 0 {
				assert.Zero(t, requeueAfter)
			} else {
				assert.Positive(t, requeueAfter)
				assert.LessOrEqual(t, requeueAfter, tt.wantRequeue)
			}

			if tt.wantDeleted {
				assert.Equal(t, []types.UID{tt.pod.UID}, deletedUIDs)
			} else {
				assert.Empty(t, deletedUIDs)
			}

			var updated slurmv1alpha1.NodeSetPowerState
			require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKeyFromObject(powerState), &updated))
			if tt.wantKept {
				assert.Len(t, updated.Spec.RecreateNodes, 1)
			} else {
				assert.Empty(t, updated.Spec.RecreateNodes)
			}
			assert.Equal(t, []int32{0, 1, 2}, updated.Spec.ActiveNodes)
		})
	}
}

func TestRecreateRequestedPodsKeepsRenewedRequests(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	past := metav1.NewTime(now.Add(-time.Hour))
	renewed := metav1.NewTime(now)
	nodeSet := &slurmv1alpha1.NodeSet{
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test-namespace"},
		Spec:       slurmv1alpha1.NodeSetSpec{Replicas: 3},
	}
	powerState := newRecreatePowerState(nodeSet,
		slurmv1alpha1.NodeRecreateRequest{Ordinal: 0, RequestedAt: past},
		slurmv1alpha1.NodeRecreateRequest{Ordinal: 1, RequestedAt: past},
	)

	patches := 0
	r, fakeClient := newStaticRecreateReconciler(t, interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			patches++
			if patches == 1 {
				concurrent := &slurmv1alpha1.NodeSetPowerState{}
				require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), concurrent))
				concurrent.Spec.RecreateNodes = []slurmv1alpha1.NodeRecreateRequest{
					{Ordinal: 0, RequestedAt: past},
					{Ordinal: 1, RequestedAt: renewed},
					{Ordinal: 2, RequestedAt: renewed},
				}
				require.NoError(t, c.Update(ctx, concurrent))
				return apierrors.NewConflict(schema.GroupResource{Resource: "nodesetpowerstates"}, obj.GetName(), errors.New("modified"))
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	},
		powerState,
		newRecreateWorkerPod(0, past.Add(-time.Hour)),
		newRecreateWorkerPod(1, past.Add(-time.Hour)),
	)

	_, err := r.recreateRequestedPods(context.Background(), nodeSet, &slurmv1.SlurmCluster{})
	require.NoError(t, err)
	assert.Equal(t, 2, patches)

	var updated slurmv1alpha1.NodeSetPowerState
	require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKeyFromObject(powerState), &updated))
	require.Len(t, updated.Spec.RecreateNodes, 2)
	for i, ordinal := range []int32{1, 2} {
		assert.Equal(t, ordinal, updated.Spec.RecreateNodes[i].Ordinal)
		assert.True(t, renewed.Equal(&updated.Spec.RecreateNodes[i].RequestedAt))
	}
}

func TestRecreateRequestedPodsLimitsRequestsPerReconcile(t *testing.T) {
	past := metav1.NewTime(time.Now().Add(-time.Hour))
	nodeSet := &slurmv1alpha1.NodeSet{
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test-namespace"},
		Spec:       slurmv1alpha1.NodeSetSpec{Replicas: maxRecreateRequestsPerReconcile + 1},
	}
	var requests []slurmv1alpha1.NodeRecreateRequest
	for ordinal := int32(0); ordinal <= maxRecreateRequestsPerReconcile; ordinal++ {
		requests = append(requests, slurmv1alpha1.NodeRecreateRequest{Ordinal: ordinal, RequestedAt: past})
	}
	powerState := newRecreatePowerState(nodeSet, requests...)
	r, fakeClient := newStaticRecreateReconciler(t, interceptor.Funcs{}, powerState)

	requeueAfter, err := r.recreateRequestedPods(context.Background(), nodeSet, &slurmv1.SlurmCluster{})
	require.NoError(t, err)
	assert.Equal(t, recreateRetryInterval, requeueAfter)

	var updated slurmv1alpha1.NodeSetPowerState
	require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKeyFromObject(powerState), &updated))
	require.Len(t, updated.Spec.RecreateNodes, 1)
	assert.Equal(t, int32(maxRecreateRequestsPerReconcile), updated.Spec.RecreateNodes[0].Ordinal)
}

func TestRecreateRequestedPodsWithoutPowerState(t *testing.T) {
	nodeSet := &slurmv1alpha1.NodeSet{
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test-namespace"},
	}
	r, _ := newStaticRecreateReconciler(t, interceptor.Funcs{})

	requeueAfter, err := r.recreateRequestedPods(context.Background(), nodeSet, &slurmv1.SlurmCluster{})
	require.NoError(t, err)
	assert.Zero(t, requeueAfter)
}

func TestEarliestRequeue(t *testing.T) {
	tests := []struct {
		a, b, want time.Duration
	}{
		{a: 0, b: 0, want: 0},
		{a: 0, b: 5, want: 5},
		{a: 5, b: 0, want: 5},
		{a: 5, b: 3, want: 3},
		{a: 3, b: 5, want: 3},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, earliestRequeue(tt.a, tt.b), "earliestRequeue(%v, %v)", tt.a, tt.b)
	}
}
