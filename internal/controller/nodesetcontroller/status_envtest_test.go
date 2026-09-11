//go:build envtest

package nodesetcontroller

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func TestSetUpConditionsWithAPIServer(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd", "bases", "slurm.nebius.ai_nodesets.yaml"),
		},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })

	scheme := runtime.NewScheme()
	require.NoError(t, slurmv1alpha1.AddToScheme(scheme))
	k8sClient, err := client.NewWithWatch(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	for _, existingStatus := range []bool{false, true} {
		name := "new-nodeset"
		if existingStatus {
			name = "existing-nodeset"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			nodeSet := &slurmv1alpha1.NodeSet{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Spec: slurmv1alpha1.NodeSetSpec{
					UpdateStrategy: "rollingUpdate",
					Munge: slurmv1alpha1.ContainerMungeSpec{
						Image:     slurmv1alpha1.Image{Repository: "munge"},
						Resources: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
					},
					Slurmd: slurmv1alpha1.ContainerSlurmdSpec{
						Image:     slurmv1alpha1.Image{Repository: "slurmd"},
						Resources: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
					},
				},
			}
			require.NoError(t, k8sClient.Create(ctx, nodeSet))
			key := client.ObjectKeyFromObject(nodeSet)
			raw := &unstructured.Unstructured{}
			raw.SetGroupVersionKind(slurmv1alpha1.GroupVersion.WithKind(slurmv1alpha1.KindNodeSet))
			require.NoError(t, k8sClient.Get(ctx, key, raw))
			_, found, err := unstructured.NestedInt64(raw.Object, "status", "replicas")
			require.NoError(t, err)
			require.False(t, found, "new NodeSets must exercise initialization without status.replicas")

			var wantReplicas int32
			if existingStatus {
				wantReplicas = 3
				nodeSet.Status.Replicas = wantReplicas
				nodeSet.Status.AppliedPowerState = &slurmv1alpha1.AppliedPowerState{
					UID: "power", Generation: 4, ActiveNodes: make([]int32, 0),
				}
				nodeSet.Status.SetCondition(metav1.Condition{
					Type:    slurmv1alpha1.ConditionNodeSetConfigUpdated,
					Status:  metav1.ConditionTrue,
					Reason:  "ConfigUpdated",
					Message: "Config is up to date",
				})
				require.NoError(t, k8sClient.Status().Update(ctx, nodeSet))
			}
			preservedStatus := nodeSet.Status.DeepCopy()

			writes := 0
			countingClient := interceptor.NewClient(k8sClient, interceptor.Funcs{
				SubResourceUpdate: func(ctx context.Context, c client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					writes++
					return c.SubResource(subresource).Update(ctx, obj, opts...)
				},
				SubResourcePatch: func(ctx context.Context, c client.Client, subresource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					writes++
					return c.SubResource(subresource).Patch(ctx, obj, patch, opts...)
				},
			})
			r := NewNodeSetReconciler(countingClient, scheme, record.NewFakeRecorder(10))
			require.NoError(t, r.setUpConditions(ctx, nodeSet))
			require.NoError(t, k8sClient.Get(ctx, key, nodeSet))
			assert.Equal(t, wantReplicas, nodeSet.Status.Replicas)
			assert.Equal(t, preservedStatus.AppliedPowerState, nodeSet.Status.AppliedPowerState)
			assert.Len(t, nodeSet.Status.Conditions, 6)
			for _, condition := range preservedStatus.Conditions {
				assert.Equal(t, &condition, meta.FindStatusCondition(nodeSet.Status.Conditions, condition.Type))
			}
			require.NoError(t, k8sClient.Get(ctx, key, raw))
			replicas, found, err := unstructured.NestedInt64(raw.Object, "status", "replicas")
			require.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, int64(wantReplicas), replicas)

			require.Equal(t, 1, writes)
			require.NoError(t, r.setUpConditions(ctx, nodeSet))
			assert.Equal(t, 1, writes, "initialized conditions must not cause additional API writes")
		})
	}
}
