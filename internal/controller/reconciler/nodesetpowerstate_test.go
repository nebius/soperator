package reconciler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestPowerStateReconcileSkipsUnchangedPatch(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	state := &slurmv1alpha1.NodeSetPowerState{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test"}, Spec: slurmv1alpha1.NodeSetPowerStateSpec{NodeSetRef: "worker", ActiveNodes: []int32{1, 5}}}
	patches := 0
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(state).WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			patches++
			return c.Patch(ctx, obj, patch, opts...)
		},
	}).Build()
	r := NewNodeSetPowerStateReconciler(NewReconciler(c, scheme, nil))
	desired := state.DeepCopy()
	desired.Spec.ActiveNodes = nil
	owner := &slurmv1alpha1.NodeSet{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "test", UID: "owner"}}
	require.NoError(t, r.Reconcile(context.Background(), owner, desired))
	require.Zero(t, patches)
	desired.Spec.NodeSetRef = "corrected"
	require.NoError(t, r.Reconcile(context.Background(), owner, desired))
	require.Equal(t, 1, patches)
	current := &slurmv1alpha1.NodeSetPowerState{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(state), current))
	require.Equal(t, []int32{1, 5}, current.Spec.ActiveNodes)
}
