package topologyconfcontroller

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
)

// rdNameForNamespace returns ResourceDistribution name for a given namespace
func rdNameForNamespace(namespace string) string {
	return ResourceDistributionName + "-" + namespace
}

func TestTopologyDistributionReconciler_HasNamespaceWithEphemeralNodeSets(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, kruisev1alpha1.AddToScheme(scheme))

	ephemeralTrue := true
	ephemeralFalse := false

	tests := []struct {
		name      string
		namespace string
		nodeSets  []slurmv1alpha1.NodeSet
		want      bool
	}{
		{
			name:      "no nodesets in namespace",
			namespace: "slurm-cluster",
			nodeSets:  []slurmv1alpha1.NodeSet{},
			want:      false,
		},
		{
			name:      "no ephemeral nodesets in namespace",
			namespace: "slurm-cluster",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ns1", Namespace: "slurm-cluster"},
					Spec:       slurmv1alpha1.NodeSetSpec{EphemeralNodes: &ephemeralFalse},
				},
			},
			want: false,
		},
		{
			name:      "one ephemeral nodeset in namespace",
			namespace: "slurm-cluster",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ns1", Namespace: "slurm-cluster"},
					Spec:       slurmv1alpha1.NodeSetSpec{EphemeralNodes: &ephemeralTrue},
				},
			},
			want: true,
		},
		{
			name:      "ephemeral nodeset in different namespace",
			namespace: "slurm-cluster",
			nodeSets: []slurmv1alpha1.NodeSet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ns1", Namespace: "other-namespace"},
					Spec:       slurmv1alpha1.NodeSetSpec{EphemeralNodes: &ephemeralTrue},
				},
			},
			want: false,
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
			want: true,
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
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, 0, len(tt.nodeSets))
			for i := range tt.nodeSets {
				objects = append(objects, &tt.nodeSets[i])
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			reconciler := NewTopologyDistributionReconciler(fakeClient, scheme, "soperator-system")

			ctx := context.Background()
			hasEphemeral, err := reconciler.hasNamespaceWithEphemeralNodeSets(ctx, tt.namespace)
			require.NoError(t, err)
			assert.Equal(t, tt.want, hasEphemeral)
		})
	}
}

func TestTopologyDistributionReconciler_EnsureResourceDistribution(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, kruisev1alpha1.AddToScheme(scheme))

	sourceConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      consts.ConfigMapNameTopologyNodeLabels,
			Namespace: "soperator-system",
		},
		Data: map[string]string{
			"node-1": `{"tier-1":"leaf01","tier-2":"spine01"}`,
		},
	}

	tests := []struct {
		name            string
		targetNamespace string
		existingRD      *kruisev1alpha1.ResourceDistribution
		wantCreate      bool
		wantUpdate      bool
	}{
		{
			name:            "create new ResourceDistribution",
			targetNamespace: "slurm-cluster",
			existingRD:      nil,
			wantCreate:      true,
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
								{Name: "slurm-cluster"},
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
								{Name: "slurm-cluster"},
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
			objects := []runtime.Object{sourceConfigMap}
			if tt.existingRD != nil {
				objects = append(objects, tt.existingRD)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			reconciler := NewTopologyDistributionReconciler(fakeClient, scheme, "soperator-system")

			ctx := context.Background()
			err := reconciler.ensureResourceDistribution(ctx, tt.targetNamespace)
			require.NoError(t, err)

			// Verify ResourceDistribution exists
			rd := &kruisev1alpha1.ResourceDistribution{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: rdNameForNamespace(tt.targetNamespace)}, rd)
			require.NoError(t, err)

			// Verify target namespace
			assert.Len(t, rd.Spec.Targets.IncludedNamespaces.List, 1)
			assert.Equal(t, tt.targetNamespace, rd.Spec.Targets.IncludedNamespaces.List[0].Name)
		})
	}
}

func TestTopologyDistributionReconciler_DeleteResourceDistributionIfExists(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kruisev1alpha1.AddToScheme(scheme))

	tests := []struct {
		name       string
		namespace  string
		existingRD *kruisev1alpha1.ResourceDistribution
		wantDelete bool
	}{
		{
			name:       "no existing ResourceDistribution",
			namespace:  "slurm-cluster",
			existingRD: nil,
			wantDelete: false,
		},
		{
			name:      "delete existing ResourceDistribution",
			namespace: "slurm-cluster",
			existingRD: &kruisev1alpha1.ResourceDistribution{
				ObjectMeta: metav1.ObjectMeta{
					Name: rdNameForNamespace("slurm-cluster"),
				},
			},
			wantDelete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []runtime.Object
			if tt.existingRD != nil {
				objects = append(objects, tt.existingRD)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			reconciler := NewTopologyDistributionReconciler(fakeClient, scheme, "soperator-system")

			ctx := context.Background()
			logger := ctrl.Log.WithName("test")
			_, err := reconciler.deleteResourceDistributionIfExists(ctx, tt.namespace, logger)
			require.NoError(t, err)

			// Verify ResourceDistribution was deleted
			rd := &kruisev1alpha1.ResourceDistribution{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: rdNameForNamespace(tt.namespace)}, rd)
			if tt.wantDelete {
				assert.Error(t, err)
			}
		})
	}
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

			reconciler := NewTopologyDistributionReconciler(fakeClient, scheme, "soperator-system")

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
					assert.Equal(t, ns, rd.Spec.Targets.IncludedNamespaces.List[i].Name)
				}
			} else {
				assert.True(t, err != nil || len(rd.Spec.Targets.IncludedNamespaces.List) == 0)
			}
		})
	}
}

func TestEqualResourceDistributions(t *testing.T) {
	tests := []struct {
		name string
		a    *kruisev1alpha1.ResourceDistribution
		b    *kruisev1alpha1.ResourceDistribution
		want bool
	}{
		{
			name: "equal with same namespaces",
			a: &kruisev1alpha1.ResourceDistribution{
				Spec: kruisev1alpha1.ResourceDistributionSpec{
					Targets: kruisev1alpha1.ResourceDistributionTargets{
						IncludedNamespaces: kruisev1alpha1.ResourceDistributionTargetNamespaces{
							List: []kruisev1alpha1.ResourceDistributionNamespace{
								{Name: "ns1"},
							},
						},
					},
				},
			},
			b: &kruisev1alpha1.ResourceDistribution{
				Spec: kruisev1alpha1.ResourceDistributionSpec{
					Targets: kruisev1alpha1.ResourceDistributionTargets{
						IncludedNamespaces: kruisev1alpha1.ResourceDistributionTargetNamespaces{
							List: []kruisev1alpha1.ResourceDistributionNamespace{
								{Name: "ns1"},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "not equal with different namespaces",
			a: &kruisev1alpha1.ResourceDistribution{
				Spec: kruisev1alpha1.ResourceDistributionSpec{
					Targets: kruisev1alpha1.ResourceDistributionTargets{
						IncludedNamespaces: kruisev1alpha1.ResourceDistributionTargetNamespaces{
							List: []kruisev1alpha1.ResourceDistributionNamespace{
								{Name: "ns1"},
							},
						},
					},
				},
			},
			b: &kruisev1alpha1.ResourceDistribution{
				Spec: kruisev1alpha1.ResourceDistributionSpec{
					Targets: kruisev1alpha1.ResourceDistributionTargets{
						IncludedNamespaces: kruisev1alpha1.ResourceDistributionTargetNamespaces{
							List: []kruisev1alpha1.ResourceDistributionNamespace{
								{Name: "ns2"},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "not equal with different length",
			a: &kruisev1alpha1.ResourceDistribution{
				Spec: kruisev1alpha1.ResourceDistributionSpec{
					Targets: kruisev1alpha1.ResourceDistributionTargets{
						IncludedNamespaces: kruisev1alpha1.ResourceDistributionTargetNamespaces{
							List: []kruisev1alpha1.ResourceDistributionNamespace{
								{Name: "ns1"},
							},
						},
					},
				},
			},
			b: &kruisev1alpha1.ResourceDistribution{
				Spec: kruisev1alpha1.ResourceDistributionSpec{
					Targets: kruisev1alpha1.ResourceDistributionTargets{
						IncludedNamespaces: kruisev1alpha1.ResourceDistributionTargetNamespaces{
							List: []kruisev1alpha1.ResourceDistributionNamespace{
								{Name: "ns1"},
								{Name: "ns2"},
							},
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := equalResourceDistributions(tt.a, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}
