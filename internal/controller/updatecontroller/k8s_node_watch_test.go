package updatecontroller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestMapK8sNodeToStatefulSets(t *testing.T) {
	ctx := context.Background()
	r, sts, pod, node := testK8sNodeRolloutReconciler(t, nil)
	duplicate := pod.DeepCopy()
	duplicate.Name = "worker-1"
	duplicate.ResourceVersion = ""
	require.NoError(t, r.Create(ctx, duplicate))
	other := pod.DeepCopy()
	other.Name = "worker-2"
	other.ResourceVersion = ""
	other.Spec.NodeName = "other-node"
	other.OwnerReferences[0].Name = "other-statefulset"
	require.NoError(t, r.Create(ctx, other))
	assert.Equal(t, []ctrl.Request{{NamespacedName: client.ObjectKeyFromObject(sts)}}, r.mapNodeToStatefulSetRequests(ctx, node))
}
