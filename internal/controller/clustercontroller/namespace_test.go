package clustercontroller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/values"
)

func TestReconcileNamespaceConfigSourceAndDisable(t *testing.T) {
	r := newTestReconciler(t)
	require.NoError(t, slurmv1alpha1.AddToScheme(r.Scheme))
	r.JailedConfig = reconciler.NewJailedConfigReconciler(r.Reconciler)
	cluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "slurm", UID: "cluster-uid"},
	}
	clusterValues := &values.SlurmCluster{
		NamespacedName:  types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
		NamespaceConfig: values.NamespaceConfig{Enabled: true},
	}
	external := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-namespace", Namespace: cluster.Namespace},
		Data:       map[string]string{"namespace.yaml": "custom configuration"},
	}
	require.NoError(t, r.Create(t.Context(), external))

	require.NoError(t, r.reconcileNamespaceConfig(t.Context(), cluster, clusterValues))
	key := types.NamespacedName{Name: naming.BuildJailedConfigNamespaceName(cluster.Name), Namespace: cluster.Namespace}
	config := &slurmv1alpha1.JailedConfig{}
	require.NoError(t, r.Get(t.Context(), key, config))
	assert.True(t, metav1.IsControlledBy(config, cluster))
	assert.Equal(t, naming.BuildConfigMapSlurmConfigsName(cluster.Name), config.Spec.ConfigMap.Name)

	clusterValues.NamespaceConfig.ConfigMapName = external.Name
	require.NoError(t, r.reconcileNamespaceConfig(t.Context(), cluster, clusterValues))
	require.NoError(t, r.Get(t.Context(), key, config))
	assert.Equal(t, external.Name, config.Spec.ConfigMap.Name)
	assert.True(t, metav1.IsControlledBy(config, cluster))

	clusterValues.NamespaceConfig.Enabled = false
	require.NoError(t, r.reconcileNamespaceConfig(t.Context(), cluster, clusterValues))
	assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), key, config)))
	require.NoError(t, r.reconcileNamespaceConfig(t.Context(), cluster, clusterValues))
	unchanged := &corev1.ConfigMap{}
	require.NoError(t, r.Get(t.Context(), types.NamespacedName{Name: external.Name, Namespace: external.Namespace}, unchanged))
	assert.Equal(t, external.Data, unchanged.Data)
	assert.Empty(t, unchanged.OwnerReferences)
}

func TestDisableNamespaceConfigPreservesOtherOwners(t *testing.T) {
	for _, ownerUID := range []types.UID{"", "other-cluster-uid"} {
		t.Run(string(ownerUID), func(t *testing.T) {
			r := newTestReconciler(t)
			require.NoError(t, slurmv1alpha1.AddToScheme(r.Scheme))
			cluster := &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "slurm", UID: "cluster-uid"},
			}
			config := &slurmv1alpha1.JailedConfig{
				ObjectMeta: metav1.ObjectMeta{Name: naming.BuildJailedConfigNamespaceName(cluster.Name), Namespace: cluster.Namespace},
			}
			if ownerUID != "" {
				otherCluster := cluster.DeepCopy()
				otherCluster.UID = ownerUID
				config.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(otherCluster, slurmv1.GroupVersion.WithKind("SlurmCluster"))}
			}
			require.NoError(t, r.Create(t.Context(), config))
			require.NoError(t, r.reconcileNamespaceConfig(t.Context(), cluster, &values.SlurmCluster{}))
			require.NoError(t, r.Get(t.Context(), types.NamespacedName{Name: config.Name, Namespace: config.Namespace}, &slurmv1alpha1.JailedConfig{}))
		})
	}
}
