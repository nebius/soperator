package updatecontroller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"nebius.ai/slurm-operator/internal/controller/reconciler"
)

func TestGetK8sNodeCordonStates(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	reads := make(map[string]int)
	kubeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cordoned"}, Spec: corev1.NodeSpec{Unschedulable: true}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "schedulable"}},
	).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			reads[key.Name]++
			return c.Get(ctx, key, obj, opts...)
		},
	}).Build()
	r := &RollingUpdateReconciler{Reconciler: &reconciler.Reconciler{Client: kubeClient}}
	pods := []corev1.Pod{
		{Spec: corev1.PodSpec{NodeName: "cordoned"}},
		{Spec: corev1.PodSpec{NodeName: "cordoned"}},
		{Spec: corev1.PodSpec{NodeName: "schedulable"}},
		{Spec: corev1.PodSpec{NodeName: "deleted"}},
		{},
	}
	states, err := r.getK8sNodeCordonStates(ctx, pods)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"cordoned": true, "schedulable": false, "deleted": false}, states)
	assert.Equal(t, map[string]int{"cordoned": 1, "schedulable": 1, "deleted": 1}, reads)

	states, err = r.getK8sNodeCordonStates(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, states)
}
