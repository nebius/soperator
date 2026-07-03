package soperatorchecks

import (
	"context"
	"testing"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
)

const (
	prologTestNamespace   = "test-ns"
	prologTestClusterName = "test-cluster"
)

func newPrologTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, slurmv1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, kruisev1b1.AddToScheme(scheme))
	return scheme
}

func newPrologController(scheme *runtime.Scheme, objects ...client.Object) *ActiveCheckPrologReconciler {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	return &ActiveCheckPrologReconciler{
		Reconciler: reconciler.NewReconciler(fakeClient, scheme, record.NewFakeRecorder(10)),
	}
}

func prologRequest() ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      prologTestClusterName,
			Namespace: prologTestNamespace,
		},
	}
}

func newSlurmClusterObj(namespace, name string) *slurmv1.SlurmCluster {
	return &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

// legacyControllerSTS simulates a pre-existing cluster whose StatefulSet was created
// under the old unprefixed naming scheme. ResolveWorkloadNamePrefix returns "" for such clusters.
func legacyControllerSTS(namespace, clusterName string) *kruisev1b1.StatefulSet {
	return &kruisev1b1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      consts.ComponentTypeController.String(),
			Namespace: namespace,
			Labels: map[string]string{
				consts.LabelInstanceKey: clusterName,
			},
		},
	}
}

// TestPrologReconcile_ClusterNotFound verifies that reconciliation against a missing
// SlurmCluster returns no error and creates no resources.
func TestPrologReconcile_ClusterNotFound(t *testing.T) {
	scheme := newPrologTestScheme(t)
	r := newPrologController(scheme)

	_, err := r.Reconcile(context.Background(), prologRequest())
	require.NoError(t, err)

	jcList := &v1alpha1.JailedConfigList{}
	require.NoError(t, r.Client.List(context.Background(), jcList))
	require.Empty(t, jcList.Items)
}

// TestPrologReconcile_NewCluster_PrefixedNameAndLabel verifies that for a new cluster
// (no legacy StatefulSet present) the JailedConfig and ConfigMap are created with a
// cluster-prefixed name and that LabelInstanceKey is set on the JailedConfig.
func TestPrologReconcile_NewCluster_PrefixedNameAndLabel(t *testing.T) {
	scheme := newPrologTestScheme(t)
	r := newPrologController(scheme, newSlurmClusterObj(prologTestNamespace, prologTestClusterName))

	_, err := r.Reconcile(context.Background(), prologRequest())
	require.NoError(t, err)

	expectedName := prologTestClusterName + "-" + consts.ConfigMapNameActiveCheckPrologScript

	cm := &corev1.ConfigMap{}
	require.NoError(t, r.Client.Get(context.Background(), types.NamespacedName{
		Name:      expectedName,
		Namespace: prologTestNamespace,
	}, cm))
	require.Contains(t, cm.Data, consts.ConfigMapKeyActiveCheckPrologScript)

	jc := &v1alpha1.JailedConfig{}
	require.NoError(t, r.Client.Get(context.Background(), types.NamespacedName{
		Name:      expectedName,
		Namespace: prologTestNamespace,
	}, jc))
	require.Equal(t, prologTestClusterName, jc.Labels[consts.LabelInstanceKey])
	require.Equal(t, expectedName, jc.Spec.ConfigMap.Name)
}

// TestPrologReconcile_LegacyCluster_UnprefixedNameAndLabel verifies that for a legacy
// cluster (controller StatefulSet exists with the old unprefixed name) the JailedConfig
// and ConfigMap are created without a name prefix, but LabelInstanceKey is still set.
func TestPrologReconcile_LegacyCluster_UnprefixedNameAndLabel(t *testing.T) {
	scheme := newPrologTestScheme(t)
	r := newPrologController(scheme,
		newSlurmClusterObj(prologTestNamespace, prologTestClusterName),
		legacyControllerSTS(prologTestNamespace, prologTestClusterName),
	)

	_, err := r.Reconcile(context.Background(), prologRequest())
	require.NoError(t, err)

	expectedName := consts.ConfigMapNameActiveCheckPrologScript

	cm := &corev1.ConfigMap{}
	require.NoError(t, r.Client.Get(context.Background(), types.NamespacedName{
		Name:      expectedName,
		Namespace: prologTestNamespace,
	}, cm))
	require.Contains(t, cm.Data, consts.ConfigMapKeyActiveCheckPrologScript)

	jc := &v1alpha1.JailedConfig{}
	require.NoError(t, r.Client.Get(context.Background(), types.NamespacedName{
		Name:      expectedName,
		Namespace: prologTestNamespace,
	}, jc))
	require.Equal(t, prologTestClusterName, jc.Labels[consts.LabelInstanceKey])
	require.Equal(t, expectedName, jc.Spec.ConfigMap.Name)
}
