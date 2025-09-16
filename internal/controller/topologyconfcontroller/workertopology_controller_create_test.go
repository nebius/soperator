package topologyconfcontroller

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
)

func TestWorkerTopologyReconciler_createDefaultTopologyResources(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(slurmv1.AddToScheme(scheme))
	utilruntime.Must(kruisev1b1.AddToScheme(scheme))

	namespace := "test-namespace"
	clusterName := "test-cluster"

	// Create a test SlurmCluster
	workerSize := int32(3)
	slurmCluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: slurmv1.SlurmClusterSpec{
			SlurmNodes: slurmv1.SlurmNodes{
				Worker: slurmv1.SlurmNodeWorker{
					SlurmNode: slurmv1.SlurmNode{
						Size: workerSize,
					},
				},
			},
		},
	}

	tests := []struct {
		name            string
		existingObjects []client.Object
		expectedError   bool
		errorContains   string
	}{
		{
			name: "No existing resources - should create both successfully",
			existingObjects: []client.Object{
				slurmCluster,
			},
			expectedError: false,
		},
		{
			name: "ConfigMap already exists - should not return error",
			existingObjects: []client.Object{
				slurmCluster,
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:            consts.ConfigMapNameTopologyConfig,
						Namespace:       namespace,
						ResourceVersion: "1000",
					},
					Data: map[string]string{
						consts.ConfigMapKeyTopologyConfig: "existing config",
					},
				},
			},
			expectedError: false,
		},
		{
			name: "JailedConfig already exists - should not return error",
			existingObjects: []client.Object{
				slurmCluster,
				&v1alpha1.JailedConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:            consts.ConfigMapNameTopologyConfig,
						Namespace:       namespace,
						ResourceVersion: "1000",
					},
					Spec: v1alpha1.JailedConfigSpec{
						ConfigMap: v1alpha1.ConfigMapReference{
							Name: consts.ConfigMapNameTopologyConfig,
						},
						Items: []corev1.KeyToPath{
							{
								Key:  consts.ConfigMapKeyTopologyConfig,
								Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeyTopologyConfig),
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name: "Both resources already exist - should not return error",
			existingObjects: []client.Object{
				slurmCluster,
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:            consts.ConfigMapNameTopologyConfig,
						Namespace:       namespace,
						ResourceVersion: "1000",
					},
					Data: map[string]string{
						consts.ConfigMapKeyTopologyConfig: "existing config",
					},
				},
				&v1alpha1.JailedConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:            consts.ConfigMapNameTopologyConfig,
						Namespace:       namespace,
						ResourceVersion: "1000",
					},
					Spec: v1alpha1.JailedConfigSpec{
						ConfigMap: v1alpha1.ConfigMapReference{
							Name: consts.ConfigMapNameTopologyConfig,
						},
						Items: []corev1.KeyToPath{
							{
								Key:  consts.ConfigMapKeyTopologyConfig,
								Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeyTopologyConfig),
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name:            "SlurmCluster does not exist - should fail",
			existingObjects: []client.Object{
				// No SlurmCluster object
			},
			expectedError: true,
			errorContains: "get SlurmCluster for fallback topology",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjects...).
				Build()

			reconciler := &WorkerTopologyReconciler{
				BaseReconciler: BaseReconciler{
					Client: fakeClient,
					Scheme: scheme,
				},
				namespace: namespace,
			}

			ctx := context.Background()
			err := reconciler.createDefaultTopologyResources(ctx, namespace, clusterName)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)

				// Verify ConfigMap exists
				var configMap corev1.ConfigMap
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      consts.ConfigMapNameTopologyConfig,
					Namespace: namespace,
				}, &configMap)
				assert.NoError(t, err)
				assert.Contains(t, configMap.Data, consts.ConfigMapKeyTopologyConfig)

				// Verify JailedConfig exists
				var jailedConfig v1alpha1.JailedConfig
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      consts.ConfigMapNameTopologyConfig,
					Namespace: namespace,
				}, &jailedConfig)
				assert.NoError(t, err)
				assert.Equal(t, consts.ConfigMapNameTopologyConfig, jailedConfig.Spec.ConfigMap.Name)
				assert.Len(t, jailedConfig.Spec.Items, 1)
				assert.Equal(t, consts.ConfigMapKeyTopologyConfig, jailedConfig.Spec.Items[0].Key)
				assert.Equal(t, filepath.Join("/etc/slurm/", consts.ConfigMapKeyTopologyConfig), jailedConfig.Spec.Items[0].Path)
			}
		})
	}
}
