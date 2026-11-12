package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func TestRequestNodeRecreationIsIdempotent(t *testing.T) {
	earlier := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	requestedAt := metav1.NewTime(earlier.Add(time.Hour))
	state := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test"}, Spec: slurmv1alpha1.NodeSetPowerStateSpec{
		ActiveNodes:   []int32{0, 1, 2, 3},
		RecreateNodes: []slurmv1alpha1.NodeRecreateRequest{{Ordinal: 3, RequestedAt: earlier}},
	}}
	writes := 0
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(state).WithInterceptorFuncs(interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			writes++
			return c.Update(ctx, obj, opts...)
		},
	}).Build()
	for range 2 {
		require.NoError(t, requestNodeRecreation(context.Background(), c, "test", "worker", []int32{3, 1, 1}, requestedAt))
	}
	require.Equal(t, 1, writes)

	result := &slurmv1alpha1.NodeSetPowerState{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(state), result))
	require.Equal(t, []int32{0, 1, 2, 3}, result.Spec.ActiveNodes)
	require.Len(t, result.Spec.RecreateNodes, 2)
	for i, ordinal := range []int32{1, 3} {
		require.Equal(t, ordinal, result.Spec.RecreateNodes[i].Ordinal)
		require.True(t, requestedAt.Equal(&result.Spec.RecreateNodes[i].RequestedAt))
	}
}

func TestAddRecreateRequestsKeepsLaterRequest(t *testing.T) {
	later := metav1.NewTime(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC))
	state := &slurmv1alpha1.NodeSetPowerState{Spec: slurmv1alpha1.NodeSetPowerStateSpec{
		RecreateNodes: []slurmv1alpha1.NodeRecreateRequest{{Ordinal: 3, RequestedAt: later}},
	}}
	require.False(t, addRecreateRequests(state, []int32{3}, metav1.NewTime(later.Add(-time.Hour))))
	require.True(t, later.Equal(&state.Spec.RecreateNodes[0].RequestedAt))
}

func TestRequestNodeRecreationRetriesFreshState(t *testing.T) {
	requestedAt := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	state := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test"}}
	writes := 0
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(state).WithInterceptorFuncs(interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			writes++
			if writes == 1 {
				concurrent := &slurmv1alpha1.NodeSetPowerState{}
				require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), concurrent))
				concurrent.Spec.RecreateNodes = []slurmv1alpha1.NodeRecreateRequest{{Ordinal: 7, RequestedAt: requestedAt}}
				require.NoError(t, c.Update(ctx, concurrent))
				return apierrors.NewConflict(schema.GroupResource{Group: "slurm.nebius.ai", Resource: "nodesetpowerstates"}, obj.GetName(), nil)
			}
			return c.Update(ctx, obj, opts...)
		},
	}).Build()
	require.NoError(t, requestNodeRecreation(context.Background(), c, "test", "worker", []int32{1}, requestedAt))
	require.Equal(t, 2, writes)

	result := &slurmv1alpha1.NodeSetPowerState{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(state), result))
	require.Len(t, result.Spec.RecreateNodes, 2)
	require.Equal(t, int32(1), result.Spec.RecreateNodes[0].Ordinal)
	require.Equal(t, int32(7), result.Spec.RecreateNodes[1].Ordinal)
}

func TestRequestNodeRecreationWaitsForControllerCreation(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := requestNodeRecreation(ctx, c, "test", "worker", []int32{1}, metav1.Now())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "wait for operator to create NodeSetPowerState test/worker")
}
