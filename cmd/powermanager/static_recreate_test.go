package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
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

func TestRequestNodeRecreationDetectsPrunedField(t *testing.T) {
	requestedAt := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	state := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test"}}
	writes := 0
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(state).WithInterceptorFuncs(interceptor.Funcs{
		// Mimic an API server whose CRD has no spec.recreateNodes.
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			writes++
			obj.(*slurmv1alpha1.NodeSetPowerState).Spec.RecreateNodes = nil
			return c.Update(ctx, obj, opts...)
		},
	}).Build()

	err := requestNodeRecreation(context.Background(), c, "test", "worker", []int32{1}, requestedAt)
	require.ErrorContains(t, err, "the API server dropped spec.recreateNodes")
	require.Equal(t, 1, writes, "a pruned field must not be retried")
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

func TestStartStaticRecreationsRequestsOnlyStaticNodeSets(t *testing.T) {
	nodeSets := map[string]*slurmv1alpha1.NodeSet{
		"cpu":  {ObjectMeta: metav1.ObjectMeta{Name: "cpu", Namespace: "test"}},
		"cpu2": {ObjectMeta: metav1.ObjectMeta{Name: "cpu2", Namespace: "test"}, Spec: slurmv1alpha1.NodeSetSpec{EphemeralNodes: ptr.To(false)}},
		"gpu":  {ObjectMeta: metav1.ObjectMeta{Name: "gpu", Namespace: "test"}, Spec: slurmv1alpha1.NodeSetSpec{EphemeralNodes: ptr.To(true)}},
	}
	var powerStates []client.Object
	for name := range nodeSets {
		powerStates = append(powerStates, &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test"}})
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(powerStates...).Build()

	before := time.Now().Truncate(time.Second)
	var requests sync.WaitGroup
	startStaticRecreations(context.Background(), &requests, c, "test", nodeSets,
		map[string][]int32{"cpu": {2}, "cpu2": {0}, "gpu": {1}, "gone": {0}})
	requests.Wait()
	after := time.Now()

	for name, ordinal := range map[string]int32{"cpu": 2, "cpu2": 0} {
		result := &slurmv1alpha1.NodeSetPowerState{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: "test", Name: name}, result))
		require.Len(t, result.Spec.RecreateNodes, 1, name)
		require.Equal(t, ordinal, result.Spec.RecreateNodes[0].Ordinal, name)
		requestedAt := result.Spec.RecreateNodes[0].RequestedAt.Time
		require.False(t, requestedAt.Before(before), name)
		require.False(t, requestedAt.After(after), name)
	}

	result := &slurmv1alpha1.NodeSetPowerState{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: "test", Name: "gpu"}, result))
	require.Empty(t, result.Spec.RecreateNodes)
}

func TestApplyPowerActionRunsStaticRequestsBesideEphemeralUpdates(t *testing.T) {
	tests := []struct {
		name            string
		nodes           string
		resume          bool
		failEphemeral   bool
		wantErr         bool
		wantGPUActive   []int32
		wantCPURequests []int32
	}{
		{
			name:            "suspend requests static recreation and deactivates ephemeral nodes",
			nodes:           "cpu-2,gpu-1",
			wantGPUActive:   []int32{0},
			wantCPURequests: []int32{2},
		},
		{
			name:            "failed ephemeral update still lets the static request finish",
			nodes:           "cpu-2,gpu-1",
			failEphemeral:   true,
			wantErr:         true,
			wantGPUActive:   []int32{0, 1},
			wantCPURequests: []int32{2},
		},
		{
			name:          "resume activates ephemeral nodes only",
			nodes:         "cpu-2,gpu-2",
			resume:        true,
			wantGPUActive: []int32{0, 1, 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []client.Object{
				&slurmv1alpha1.NodeSet{ObjectMeta: metav1.ObjectMeta{Name: "cpu", Namespace: "test"}},
				&slurmv1alpha1.NodeSet{ObjectMeta: metav1.ObjectMeta{Name: "gpu", Namespace: "test"}, Spec: slurmv1alpha1.NodeSetSpec{EphemeralNodes: ptr.To(true)}},
				&slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{Name: "cpu", Namespace: "test"}, Spec: slurmv1alpha1.NodeSetPowerStateSpec{ActiveNodes: []int32{0, 1, 2}}},
				&slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{Name: "gpu", Namespace: "test"}, Spec: slurmv1alpha1.NodeSetPowerStateSpec{ActiveNodes: []int32{0, 1}}},
			}
			// The static request is stored only after the ephemeral update, so a return without waiting
			// for it would leave the request missing.
			ephemeralUpdated := make(chan struct{})
			var closeOnce sync.Once
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if obj.GetName() == "cpu" {
						<-ephemeralUpdated
						time.Sleep(50 * time.Millisecond)
						return c.Update(ctx, obj, opts...)
					}
					defer closeOnce.Do(func() { close(ephemeralUpdated) })
					if tt.failEphemeral {
						return apierrors.NewForbidden(schema.GroupResource{Group: "slurm.nebius.ai", Resource: "nodesetpowerstates"}, obj.GetName(), nil)
					}
					return c.Update(ctx, obj, opts...)
				},
			}).Build()

			err := applyPowerAction(context.Background(), c, "test", tt.nodes, tt.resume)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			gpu := &slurmv1alpha1.NodeSetPowerState{}
			require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: "test", Name: "gpu"}, gpu))
			require.Equal(t, tt.wantGPUActive, gpu.Spec.ActiveNodes)

			cpu := &slurmv1alpha1.NodeSetPowerState{}
			require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: "test", Name: "cpu"}, cpu))
			var requested []int32
			for _, request := range cpu.Spec.RecreateNodes {
				requested = append(requested, request.Ordinal)
			}
			require.Equal(t, tt.wantCPURequests, requested)
			require.Equal(t, []int32{0, 1, 2}, cpu.Spec.ActiveNodes)
		})
	}
}
