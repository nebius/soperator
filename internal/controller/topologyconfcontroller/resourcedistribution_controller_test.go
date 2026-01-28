package topologyconfcontroller_test

import (
	"context"
	"testing"

	kruisev1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/topologyconfcontroller"
)

// rdNameForNamespace returns ResourceDistribution name for a given namespace
func rdNameForNamespace(namespace string) string {
	return topologyconfcontroller.ResourceDistributionName + "-" + namespace
}

func TestTopologyDistributionReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, kruisev1alpha1.AddToScheme(scheme))

	ephemeralTrue := true
	ephemeralFalse := false

	tests := []struct {
		name                 string
		namespace            string
		nodeSets             []slurmv1alpha1.NodeSet
		existingRD           *kruisev1alpha1.ResourceDistribution
		wantRDExists         bool
		wantTargetNamespaces []string
	}{
		{
			name:      "create RD when ephemeral nodeset exists",
			namespace: "slurm-cluster",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ephemeral-workers", Namespace: "slurm-cluster"},
					Spec:       slurmv1alpha1.NodeSetSpec{EphemeralNodes: &ephemeralTrue},
				},
			},
			wantRDExists:         true,
			wantTargetNamespaces: []string{"slurm-cluster"},
		},
		{
			name:      "skip when no ephemeral nodesets",
			namespace: "slurm-cluster",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "regular-workers", Namespace: "slurm-cluster"},
					Spec:       slurmv1alpha1.NodeSetSpec{EphemeralNodes: &ephemeralFalse},
				},
			},
			wantRDExists: false,
		},
		{
			name:         "skip when no nodesets in namespace",
			namespace:    "slurm-cluster",
			nodeSets:     []slurmv1alpha1.NodeSet{},
			wantRDExists: false,
		},
		{
			name:      "delete RD when ephemeral nodeset is removed",
			namespace: "slurm-cluster",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "regular-workers", Namespace: "slurm-cluster"},
					Spec:       slurmv1alpha1.NodeSetSpec{EphemeralNodes: &ephemeralFalse},
				},
			},
			existingRD: &kruisev1alpha1.ResourceDistribution{
				ObjectMeta: metav1.ObjectMeta{
					Name: rdNameForNamespace("slurm-cluster"),
				},
				Spec: kruisev1alpha1.ResourceDistributionSpec{
					Targets: kruisev1alpha1.ResourceDistributionTargets{
						IncludedNamespaces: kruisev1alpha1.ResourceDistributionTargetNamespaces{
							List: []kruisev1alpha1.ResourceDistributionNamespace{
								{Name: "slurm-cluster"},
							},
						},
					},
				},
			},
			wantRDExists: false,
		},
		{
			name:      "no nodesets in namespace - ephemeral in different namespace",
			namespace: "slurm-cluster",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ns1", Namespace: "other-namespace"},
					Spec:       slurmv1alpha1.NodeSetSpec{EphemeralNodes: &ephemeralTrue},
				},
			},
			wantRDExists: false,
		},
		{
			name:      "mixed nodesets - one ephemeral",
			namespace: "slurm-cluster",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ns1", Namespace: "slurm-cluster"},
					Spec:       slurmv1alpha1.NodeSetSpec{EphemeralNodes: &ephemeralFalse},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ns2", Namespace: "slurm-cluster"},
					Spec:       slurmv1alpha1.NodeSetSpec{EphemeralNodes: &ephemeralTrue},
				},
			},
			wantRDExists:         true,
			wantTargetNamespaces: []string{"slurm-cluster"},
		},
		{
			name:      "nil ephemeralNodes treated as false",
			namespace: "slurm-cluster",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ns1", Namespace: "slurm-cluster"},
					Spec:       slurmv1alpha1.NodeSetSpec{},
				},
			},
			wantRDExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      consts.ConfigMapNameTopologyNodeLabels,
					Namespace: "soperator-system",
				},
				Data: map[string]string{
					"node-1": `{"tier-1":"leaf01"}`,
				},
			}

			objects := []runtime.Object{sourceConfigMap}
			for i := range tt.nodeSets {
				objects = append(objects, &tt.nodeSets[i])
			}
			if tt.existingRD != nil {
				objects = append(objects, tt.existingRD)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			reconciler := topologyconfcontroller.NewTopologyDistributionReconciler(fakeClient, scheme, "soperator-system")

			ctx := context.Background()
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "trigger", Namespace: tt.namespace},
			})
			require.NoError(t, err)
			assert.False(t, result.Requeue)

			// Verify ResourceDistribution state
			rd := &kruisev1alpha1.ResourceDistribution{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: rdNameForNamespace(tt.namespace)}, rd)
			if tt.wantRDExists {
				require.NoError(t, err)
				assert.Len(t, rd.Spec.Targets.IncludedNamespaces.List, len(tt.wantTargetNamespaces))
				for i, ns := range tt.wantTargetNamespaces {
					assert.Equal(t, rdNameForNamespace(ns), rd.Spec.Targets.IncludedNamespaces.List[i].Name)
				}
			} else {
				assert.True(t, err != nil || len(rd.Spec.Targets.IncludedNamespaces.List) == 0)
			}
		})
	}
}

func TestTopologyDistributionReconciler_UpdateExistingRD(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, kruisev1alpha1.AddToScheme(scheme))

	ephemeralTrue := true

	tests := []struct {
		name            string
		targetNamespace string
		existingRD      *kruisev1alpha1.ResourceDistribution
		wantUpdate      bool
	}{
		{
			name:            "create new ResourceDistribution",
			targetNamespace: "slurm-cluster",
			existingRD:      nil,
			wantUpdate:      false,
		},
		{
			name:            "update existing ResourceDistribution with different namespace",
			targetNamespace: "slurm-cluster-2",
			existingRD: &kruisev1alpha1.ResourceDistribution{
				ObjectMeta: metav1.ObjectMeta{
					Name: rdNameForNamespace("slurm-cluster-2"),
				},
				Spec: kruisev1alpha1.ResourceDistributionSpec{
					Targets: kruisev1alpha1.ResourceDistributionTargets{
						IncludedNamespaces: kruisev1alpha1.ResourceDistributionTargetNamespaces{
							List: []kruisev1alpha1.ResourceDistributionNamespace{
								{Name: rdNameForNamespace("slurm-cluster")},
							},
						},
					},
				},
			},
			wantUpdate: true,
		},
		{
			name:            "no update when namespace matches",
			targetNamespace: "slurm-cluster",
			existingRD: &kruisev1alpha1.ResourceDistribution{
				ObjectMeta: metav1.ObjectMeta{
					Name: rdNameForNamespace("slurm-cluster"),
				},
				Spec: kruisev1alpha1.ResourceDistributionSpec{
					Targets: kruisev1alpha1.ResourceDistributionTargets{
						IncludedNamespaces: kruisev1alpha1.ResourceDistributionTargetNamespaces{
							List: []kruisev1alpha1.ResourceDistributionNamespace{
								{Name: rdNameForNamespace("slurm-cluster")},
							},
						},
					},
				},
			},
			wantUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      consts.ConfigMapNameTopologyNodeLabels,
					Namespace: "soperator-system",
				},
				Data: map[string]string{
					"node-1": `{"tier-1":"leaf01","tier-2":"spine01"}`,
				},
			}

			nodeSet := &slurmv1alpha1.NodeSet{
				ObjectMeta: metav1.ObjectMeta{Name: "ephemeral-workers", Namespace: tt.targetNamespace},
				Spec:       slurmv1alpha1.NodeSetSpec{EphemeralNodes: &ephemeralTrue},
			}

			objects := []runtime.Object{sourceConfigMap, nodeSet}
			if tt.existingRD != nil {
				objects = append(objects, tt.existingRD)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			reconciler := topologyconfcontroller.NewTopologyDistributionReconciler(fakeClient, scheme, "soperator-system")

			ctx := context.Background()
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "trigger", Namespace: tt.targetNamespace},
			})
			require.NoError(t, err)

			// Verify ResourceDistribution exists with correct target
			rd := &kruisev1alpha1.ResourceDistribution{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: rdNameForNamespace(tt.targetNamespace)}, rd)
			require.NoError(t, err)

			// Verify target namespace is correct
			assert.Len(t, rd.Spec.Targets.IncludedNamespaces.List, 1)
			assert.Equal(t, rdNameForNamespace(tt.targetNamespace), rd.Spec.Targets.IncludedNamespaces.List[0].Name)
		})
	}
}
