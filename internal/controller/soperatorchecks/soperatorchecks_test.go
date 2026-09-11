package soperatorchecks

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

// newTrimmedCacheClient mimics the manager client in soperatorchecks: reads come back with the
// Node cache transform already applied, so a caller that writes the object back must not carry
// the truncation to the API server. stopTrimming lifts the transform so assertions can observe
// what the write actually stored.
func newTrimmedCacheClient(t *testing.T, objects ...ctrlclient.Object) (c ctrlclient.Client, stopTrimming func()) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	trim := true

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&corev1.Node{}).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(
				ctx context.Context,
				c ctrlclient.WithWatch,
				key ctrlclient.ObjectKey,
				obj ctrlclient.Object,
				opts ...ctrlclient.GetOption,
			) error {
				if err := c.Get(ctx, key, obj, opts...); err != nil {
					return err
				}
				if !trim {
					return nil
				}
				_, err := controllerconfig.TrimNodeForCache(obj)
				return err
			},
		}).
		Build(), func() { trim = false }
}

func nodeWithImages(conditions ...corev1.NodeCondition) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-node"},
		Status: corev1.NodeStatus{
			Conditions: conditions,
			Images: []corev1.ContainerImage{
				{Names: []string{"cr.eu-north1.nebius.cloud/soperator:1.0.0"}, SizeBytes: 1024},
			},
		},
	}
}

func TestSetK8SNodeCondition_addingConditionKeepsTrimmedStatusFields(t *testing.T) {
	ctx := context.Background()
	k8sClient, stopTrimming := newTrimmedCacheClient(t, nodeWithImages())

	require.NoError(t, setK8SNodeCondition(ctx, k8sClient, "worker-node", newNodeCondition(
		consts.SoperatorChecksK8SNodeMaintenance,
		corev1.ConditionTrue,
		consts.ReasonNodeDraining,
		consts.MessageMaintenanceScheduled,
	)))

	stopTrimming()
	var stored corev1.Node
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: "worker-node"}, &stored))
	require.Len(t, stored.Status.Conditions, 1)
	require.Equal(t, consts.SoperatorChecksK8SNodeMaintenance, stored.Status.Conditions[0].Type)
	require.Len(t, stored.Status.Images, 1, "status.images must survive a condition write")
}

func TestSetK8SNodeCondition_unchangedConditionUpdatesHeartbeat(t *testing.T) {
	ctx := context.Background()
	staleHeartbeat := metav1.NewTime(time.Date(2026, time.April, 7, 10, 0, 0, 0, time.UTC))
	condition := newNodeCondition(
		consts.SoperatorChecksK8SNodeMaintenance,
		corev1.ConditionTrue,
		consts.ReasonNodeDraining,
		consts.MessageMaintenanceScheduled,
	)

	existing := condition
	existing.LastHeartbeatTime = staleHeartbeat
	k8sClient, stopTrimming := newTrimmedCacheClient(t, nodeWithImages(existing))

	require.NoError(t, setK8SNodeCondition(ctx, k8sClient, "worker-node", condition))

	stopTrimming()
	var stored corev1.Node
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: "worker-node"}, &stored))
	require.Len(t, stored.Status.Conditions, 1)
	require.True(t,
		stored.Status.Conditions[0].LastHeartbeatTime.After(staleHeartbeat.Time),
		"LastHeartbeatTime must actually reach the API server",
	)
}
