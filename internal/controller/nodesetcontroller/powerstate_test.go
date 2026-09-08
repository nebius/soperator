package nodesetcontroller

import (
	"context"
	"testing"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/values"
)

func readyPowerPod(name string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", OwnerReferences: []metav1.OwnerReference{{APIVersion: kruisev1b1.GroupVersion.String(), Kind: "StatefulSet", Name: "worker", UID: "sts", Controller: ptr.To(true)}}},
		Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
}

func TestPowerPodsReady(t *testing.T) {
	sts := &kruisev1b1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "worker", UID: "sts"}}
	terminating := readyPowerPod("worker-1")
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	pending := readyPowerPod("worker-1")
	pending.Status.Conditions = nil
	old := readyPowerPod("worker-1")
	old.OwnerReferences[0].UID = "old-sts"
	for _, tt := range []struct {
		name   string
		active []int32
		pods   []corev1.Pod
		ready  bool
	}{
		{name: "empty", ready: true},
		{name: "ready", active: []int32{1, 3}, pods: []corev1.Pod{readyPowerPod("worker-1"), readyPowerPod("worker-3")}, ready: true},
		{name: "missing", active: []int32{1}},
		{name: "same count wrong ordinals", active: []int32{1}, pods: []corev1.Pod{readyPowerPod("worker-2")}},
		{name: "terminating active", active: []int32{1}, pods: []corev1.Pod{terminating}},
		{name: "terminating inactive", pods: []corev1.Pod{terminating}},
		{name: "pending", active: []int32{1}, pods: []corev1.Pod{pending}},
		{name: "previous StatefulSet", active: []int32{1}, pods: []corev1.Pod{old}},
	} {
		t.Run(tt.name, func(t *testing.T) { require.Equal(t, tt.ready, powerPodsReady(sts, tt.pods, tt.active)) })
	}
}

func TestReconcilePowerStateReadyAndCleanup(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	require.NoError(t, kruisev1b1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	ns := &slurmv1alpha1.NodeSet{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test", Generation: 7}}
	sts := &kruisev1b1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test", UID: "sts", Generation: 3}, Spec: kruisev1b1.StatefulSetSpec{Replicas: ptr.To(int32(1))}, Status: kruisev1b1.StatefulSetStatus{ObservedGeneration: 3}}
	pod := readyPowerPod("worker-1")
	patches := 0
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns, sts, &pod).
		WithStatusSubresource(ns).WithIndex(&corev1.Pod{}, powerPodOwnerIndex, powerPodOwnerKeys).
		WithInterceptorFuncs(interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			patches++
			return c.SubResource(sub).Patch(ctx, obj, patch, opts...)
		}}).Build()
	r := &NodeSetReconciler{Reconciler: reconciler.NewReconciler(c, scheme, nil)}
	state := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{UID: "power", Generation: 42}, Spec: slurmv1alpha1.NodeSetPowerStateSpec{ActiveNodes: []int32{1}}}
	v := &values.SlurmNodeSet{}
	v.StatefulSet.Name = "worker"
	ready, err := r.reconcilePowerStateReady(ctx, ns, v, state)
	require.NoError(t, err)
	require.True(t, ready)
	require.Equal(t, int64(42), ns.Status.AppliedPowerState.Generation)
	require.Equal(t, []int32{1}, ns.Status.AppliedPowerState.ActiveNodes)
	require.Equal(t, int64(7), meta.FindStatusCondition(ns.Status.Conditions, slurmv1alpha1.ConditionNodeSetPowerReady).ObservedGeneration)
	_, err = r.reconcilePowerStateReady(ctx, ns, v, state)
	require.NoError(t, err)
	require.Equal(t, 1, patches, "unchanged readiness must not write status")
	state.Generation++
	state.Spec.ActiveNodes[0] = 2
	require.Equal(t, []int32{1}, ns.Status.AppliedPowerState.ActiveNodes)
	ready, err = r.reconcilePowerStateReady(ctx, ns, v, state)
	require.NoError(t, err)
	require.False(t, ready)
	require.Equal(t, []int32{2}, ns.Status.AppliedPowerState.ActiveNodes)
	require.Equal(t, metav1.ConditionFalse, meta.FindStatusCondition(ns.Status.Conditions, slurmv1alpha1.ConditionNodeSetPowerReady).Status)
	require.Len(t, ns.Status.Conditions, 1, "new actions replace the same condition")
	require.NoError(t, r.clearPowerStateReady(ctx, ns))
	require.Nil(t, ns.Status.AppliedPowerState)
	require.Empty(t, ns.Status.Conditions)
	before := patches
	require.NoError(t, r.clearPowerStateReady(ctx, ns))
	require.Equal(t, before, patches)
}

func TestReconcilePowerStateReadyInMaintenance(t *testing.T) {
	for _, tt := range []struct {
		name        string
		active      []int32
		pod         bool
		terminating bool
		ready       bool
	}{
		{name: "all pods deleted", ready: true},
		{name: "inactive pod still present", pod: true},
		{name: "inactive pod terminating", pod: true, terminating: true},
		{name: "resume is not acknowledged", active: []int32{1}, pod: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
			require.NoError(t, kruisev1b1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))
			ns := &slurmv1alpha1.NodeSet{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test", Generation: 7}}
			sts := &kruisev1b1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test", UID: "sts", Generation: 3},
				Spec:       kruisev1b1.StatefulSetSpec{Replicas: ptr.To(int32(len(tt.active)))},
				Status:     kruisev1b1.StatefulSetStatus{ObservedGeneration: 3},
			}
			patches := 0
			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns, sts).
				WithStatusSubresource(ns).WithIndex(&corev1.Pod{}, powerPodOwnerIndex, powerPodOwnerKeys).
				WithInterceptorFuncs(interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					patches++
					return c.SubResource(sub).Patch(ctx, obj, patch, opts...)
				}})
			if tt.pod {
				pod := readyPowerPod("worker-1")
				if tt.terminating {
					pod.DeletionTimestamp = ptr.To(metav1.Now())
					pod.Finalizers = []string{"test.example.com/termination"}
				}
				builder.WithObjects(&pod)
			}
			r := &NodeSetReconciler{Reconciler: reconciler.NewReconciler(builder.Build(), scheme, nil)}
			state := &slurmv1alpha1.NodeSetPowerState{
				ObjectMeta: metav1.ObjectMeta{UID: "power", Generation: 42},
				Spec:       slurmv1alpha1.NodeSetPowerStateSpec{ActiveNodes: tt.active},
			}
			v := &values.SlurmNodeSet{Maintenance: ptr.To(consts.ModeDownscale)}
			v.StatefulSet.Name = "worker"
			for range 2 {
				ready, err := r.reconcilePowerStateReady(ctx, ns, v, state)
				require.NoError(t, err)
				require.Equal(t, tt.ready, ready)
			}
			condition := meta.FindStatusCondition(ns.Status.Conditions, slurmv1alpha1.ConditionNodeSetPowerReady)
			require.NotNil(t, condition)
			require.Equal(t, tt.ready, condition.Status == metav1.ConditionTrue)
			require.Equal(t, 1, patches, "unchanged maintenance readiness must not write status again")
		})
	}
}

func TestReconcilePowerStateReadyMissingStatefulSet(t *testing.T) {
	for _, previouslyReady := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial creation", true: "invalidate stale readiness"}[previouslyReady], func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
			require.NoError(t, kruisev1b1.AddToScheme(scheme))
			ns := &slurmv1alpha1.NodeSet{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test", Generation: 7}}
			if previouslyReady {
				meta.SetStatusCondition(&ns.Status.Conditions, metav1.Condition{Type: slurmv1alpha1.ConditionNodeSetPowerReady, Status: metav1.ConditionTrue, Reason: "PodsReady", ObservedGeneration: 7})
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).WithStatusSubresource(ns).Build()
			r := &NodeSetReconciler{Reconciler: reconciler.NewReconciler(c, scheme, nil)}
			state := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{UID: "power", Generation: 42}}
			v := &values.SlurmNodeSet{}
			v.StatefulSet.Name = "worker"
			ready, err := r.reconcilePowerStateReady(ctx, ns, v, state)
			require.NoError(t, err)
			require.False(t, ready)
			stored := &slurmv1alpha1.NodeSet{}
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(ns), stored))
			condition := meta.FindStatusCondition(stored.Status.Conditions, slurmv1alpha1.ConditionNodeSetPowerReady)
			require.NotNil(t, condition)
			require.Equal(t, metav1.ConditionFalse, condition.Status)
			require.Equal(t, int64(7), condition.ObservedGeneration)
			require.Equal(t, &slurmv1alpha1.AppliedPowerState{UID: "power", Generation: 42, ActiveNodes: []int32{}}, stored.Status.AppliedPowerState)
		})
	}
}
