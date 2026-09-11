package nodesetcontroller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestReconcileWorkerDisruptionBudgetLifecycle(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, policyv1.AddToScheme(scheme))
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	kubeClient := clientfake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewNodeSetReconciler(kubeClient, scheme, record.NewFakeRecorder(10))
	owner := &slurmv1alpha1.NodeSet{ObjectMeta: metav1.ObjectMeta{Name: "gpu", Namespace: "default", UID: "nodeset-uid"}}
	nodeSet := &values.SlurmNodeSet{
		Name: owner.Name, ParentalCluster: client.ObjectKey{Namespace: owner.Namespace, Name: "cluster"},
		StatefulSet: values.StatefulSet{Name: "workers"}, UpdateStrategy: consts.UpdateStrategySlurmAwareRollingUpdate,
	}
	key := client.ObjectKey{Namespace: owner.Namespace, Name: nodeSet.StatefulSet.Name}
	pdb := &policyv1.PodDisruptionBudget{}
	require.NoError(t, r.reconcilePodDisruptionBudget(ctx, owner, nodeSet))
	require.NoError(t, r.Get(ctx, key, pdb))
	assert.True(t, metav1.IsControlledBy(pdb, owner))
	version := pdb.ResourceVersion
	require.NoError(t, r.reconcilePodDisruptionBudget(ctx, owner, nodeSet))
	require.NoError(t, r.Get(ctx, key, pdb))
	assert.Equal(t, version, pdb.ResourceVersion)

	pdb.Spec.MaxUnavailable = ptr.To(intstr.FromInt32(1))
	pdb.Spec.UnhealthyPodEvictionPolicy = ptr.To(policyv1.AlwaysAllow)
	require.NoError(t, r.Update(ctx, pdb))
	require.NoError(t, r.reconcilePodDisruptionBudget(ctx, owner, nodeSet))
	require.NoError(t, r.Get(ctx, key, pdb))
	assert.Zero(t, pdb.Spec.MaxUnavailable.IntVal)
	assert.Equal(t, policyv1.IfHealthyBudget, *pdb.Spec.UnhealthyPodEvictionPolicy)

	nodeSet.UpdateStrategy = consts.UpdateStrategyRollingUpdate
	require.NoError(t, r.reconcilePodDisruptionBudget(ctx, owner, nodeSet))
	assert.True(t, apierrors.IsNotFound(r.Get(ctx, key, pdb)))
	require.NoError(t, r.reconcilePodDisruptionBudget(ctx, owner, nodeSet))

	// A PDB owned by somebody else must survive disabling this feature.
	pdb = &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace,
		OwnerReferences: []metav1.OwnerReference{{APIVersion: slurmv1alpha1.GroupVersion.String(), Kind: "NodeSet", Name: "other", UID: "other-uid", Controller: ptr.To(true)}},
	}}
	require.NoError(t, r.Create(ctx, pdb))
	require.NoError(t, r.reconcilePodDisruptionBudget(ctx, owner, nodeSet))
	require.NoError(t, r.Get(ctx, key, pdb))
	nodeSet.UpdateStrategy = consts.UpdateStrategySlurmAwareRollingUpdate
	require.Error(t, r.reconcilePodDisruptionBudget(ctx, owner, nodeSet))
}
