package main

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func TestUpdatePowerStateIsIdempotent(t *testing.T) {
	for _, resume := range []bool{true, false} {
		t.Run(map[bool]string{true: "resume", false: "suspend"}[resume], func(t *testing.T) {
			state := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test"}, Spec: slurmv1alpha1.NodeSetPowerStateSpec{ActiveNodes: []int32{2}}}
			writes := 0
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(state).WithInterceptorFuncs(interceptor.Funcs{
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					writes++
					return c.Update(ctx, obj, opts...)
				},
			}).Build()
			for range 2 {
				result, err := updateNodeSetPowerState(context.Background(), c, "test", "worker", []int32{1, 1}, resume)
				require.NoError(t, err)
				require.True(t, ordinalsMatch(result, []int32{1}, resume))
				require.Contains(t, result.Spec.ActiveNodes, int32(2))
			}
			expected := 0
			if resume {
				expected = 1
			}
			require.Equal(t, expected, writes)
		})
	}
}

func TestUpdatePowerStateRetriesFreshState(t *testing.T) {
	state := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test"}}
	writes := 0
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(state).WithInterceptorFuncs(interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			writes++
			if writes == 1 {
				concurrent := &slurmv1alpha1.NodeSetPowerState{}
				require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), concurrent))
				concurrent.Spec.ActiveNodes = []int32{7}
				require.NoError(t, c.Update(ctx, concurrent))
				return apierrors.NewConflict(schema.GroupResource{Group: "slurm.nebius.ai", Resource: "nodesetpowerstates"}, obj.GetName(), nil)
			}
			return c.Update(ctx, obj, opts...)
		},
	}).Build()
	result, err := updateNodeSetPowerState(context.Background(), c, "test", "worker", []int32{1}, true)
	require.NoError(t, err)
	require.Equal(t, []int32{1, 7}, result.Spec.ActiveNodes)
	require.Equal(t, 2, writes)
}

func TestUpdatePowerStateWaitsForControllerCreation(t *testing.T) {
	reads := 0
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			reads++
			if reads == 2 {
				state := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}, Spec: slurmv1alpha1.NodeSetPowerStateSpec{ActiveNodes: []int32{5}}}
				require.NoError(t, c.Create(ctx, state))
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}).Build()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := updateNodeSetPowerState(ctx, c, "test", "worker", []int32{1}, true)
	require.NoError(t, err)
	require.Equal(t, []int32{1, 5}, result.Spec.ActiveNodes)
}

func TestUpdatePowerStateRetriesTransientErrors(t *testing.T) {
	for _, transient := range []error{
		apierrors.NewServiceUnavailable("try again"),
		apierrors.NewTooManyRequests("try again", 0),
		&net.DNSError{IsTimeout: true},
	} {
		for _, operation := range []string{"get", "update", "committed update"} {
			t.Run(operation+"/"+transient.Error(), func(t *testing.T) {
				state := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test"}}
				reads, writes := 0, 0
				c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(state).WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						reads++
						if operation == "get" && reads == 1 {
							return fmt.Errorf("read object: %w", transient)
						}
						return c.Get(ctx, key, obj, opts...)
					},
					Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
						writes++
						if operation != "get" && writes == 1 {
							// Another action commits while our response is lost or rejected.
							concurrent := state.DeepCopy()
							require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), concurrent))
							concurrent.Spec.ActiveNodes = []int32{7}
							if operation == "committed update" {
								concurrent.Spec.ActiveNodes = append(concurrent.Spec.ActiveNodes, 1)
							}
							require.NoError(t, c.Update(ctx, concurrent))
							return fmt.Errorf("write object: %w", transient)
						}
						return c.Update(ctx, obj, opts...)
					},
				}).Build()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				result, err := updateNodeSetPowerState(ctx, c, "test", "worker", []int32{1}, true)
				require.NoError(t, err)
				require.Contains(t, result.Spec.ActiveNodes, int32(1))
				require.Equal(t, 2, reads)
				if operation != "get" {
					require.Contains(t, result.Spec.ActiveNodes, int32(7))
				}
				if operation == "update" {
					require.Equal(t, 2, writes)
				} else {
					require.Equal(t, 1, writes)
				}
			})
		}
	}
}

func TestPowerStateTransientErrorsRespectDeadline(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return apierrors.NewServiceUnavailable("try again")
		},
	}).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := updateNodeSetPowerState(ctx, c, "test", "worker", []int32{1}, false)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestPowerStateAcknowledgement(t *testing.T) {
	target := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{UID: "power", Generation: 4}}
	good := &slurmv1alpha1.NodeSet{ObjectMeta: metav1.ObjectMeta{Generation: 3}, Status: slurmv1alpha1.NodeSetStatus{
		AppliedPowerState: &slurmv1alpha1.AppliedPowerState{UID: "power", Generation: 4},
		Conditions:        []metav1.Condition{{Type: slurmv1alpha1.ConditionNodeSetPowerReady, Status: metav1.ConditionTrue, ObservedGeneration: 3}},
	}}
	require.True(t, powerStateAcknowledged(good, target))
	for _, change := range []func(*slurmv1alpha1.NodeSet){
		func(ns *slurmv1alpha1.NodeSet) { ns.Status.AppliedPowerState = nil },
		func(ns *slurmv1alpha1.NodeSet) { ns.Status.AppliedPowerState.UID = "old" },
		func(ns *slurmv1alpha1.NodeSet) { ns.Status.AppliedPowerState.Generation = 3 },
		func(ns *slurmv1alpha1.NodeSet) { ns.Status.Conditions[0].Status = metav1.ConditionFalse },
		func(ns *slurmv1alpha1.NodeSet) { ns.Generation++ },
	} {
		ns := good.DeepCopy()
		change(ns)
		require.False(t, powerStateAcknowledged(ns, target))
	}
}

func TestPowerStateRetryHonorsCancellation(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := updateNodeSetPowerState(ctx, c, "test", "missing", []int32{1}, true)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), time.Second)
}

func TestClientRateLimits(t *testing.T) {
	config := &rest.Config{QPS: -1}
	setClientRateLimits(config, 0.001, 2)
	require.Equal(t, float32(0.001), config.QPS)
	require.Equal(t, 2, config.Burst)
	require.True(t, config.RateLimiter.TryAccept())
	require.True(t, config.RateLimiter.TryAccept())
	require.False(t, config.RateLimiter.TryAccept(), "burst must cap immediate requests")
}

func TestRetryPowerStateUpdateOutlivesBackoffSteps(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	attempts := 0
	err := retryPowerStateUpdate(ctx, wait.Backoff{Steps: 8, Duration: time.Microsecond, Factor: 2, Cap: time.Millisecond}, func() error {
		attempts++
		if attempts <= 10 {
			return apierrors.NewNotFound(schema.GroupResource{Resource: "nodesetpowerstates"}, "worker")
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 11, attempts)
}

func TestRetryPowerStateUpdateStopsOnPermanentError(t *testing.T) {
	attempts := 0
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "nodesetpowerstates"}, "worker", nil)
	err := retryPowerStateUpdate(context.Background(), wait.Backoff{Duration: time.Millisecond}, func() error {
		attempts++
		return forbidden
	})
	require.ErrorIs(t, err, forbidden)
	require.Equal(t, 1, attempts)
}
