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

	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
)

func TestWorkerTopologyReconciler_updateTopologyConfigMap_Fixed(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	namespace := "test-namespace"

	tests := []struct {
		name            string
		existingObjects []client.Object
		expectedError   bool
		errorContains   string
	}{
		{
			name:            "ConfigMap and JailedConfig do not exist - should fail",
			existingObjects: []client.Object{},
			expectedError:   true,
			errorContains:   "get ConfigMap",
		},
		{
			name: "ConfigMap exists, JailedConfig does not exist - should fail on JailedConfig get",
			existingObjects: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:            consts.ConfigMapNameTopologyConfig,
						Namespace:       namespace,
						ResourceVersion: "1000",
					},
					Data: map[string]string{
						consts.ConfigMapKeyTopologyConfig: "old config",
					},
				},
			},
			expectedError: true,
			errorContains: "get JailedConfig",
		},
		{
			name: "ConfigMap does not exist, JailedConfig exists - should fail on ConfigMap get",
			existingObjects: []client.Object{
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
			expectedError: true,
			errorContains: "get ConfigMap",
		},
		{
			name: "Both ConfigMap and JailedConfig exist - should update successfully",
			existingObjects: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:            consts.ConfigMapNameTopologyConfig,
						Namespace:       namespace,
						ResourceVersion: "1000",
					},
					Data: map[string]string{
						consts.ConfigMapKeyTopologyConfig: "old config",
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
			name: "ConfigMap exists with different data - should update data",
			existingObjects: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:            consts.ConfigMapNameTopologyConfig,
						Namespace:       namespace,
						ResourceVersion: "1000",
					},
					Data: map[string]string{
						consts.ConfigMapKeyTopologyConfig: "existing data",
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
			err := reconciler.updateTopologyConfigMap(ctx, namespace, "new-topology-config")

			if tt.expectedError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)

				// Verify ConfigMap was updated
				var updatedConfigMap corev1.ConfigMap
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      consts.ConfigMapNameTopologyConfig,
					Namespace: namespace,
				}, &updatedConfigMap)
				assert.NoError(t, err)
				assert.Equal(t, "new-topology-config", updatedConfigMap.Data[consts.ConfigMapKeyTopologyConfig])

				// Verify JailedConfig exists and has correct spec
				var updatedJailedConfig v1alpha1.JailedConfig
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      consts.ConfigMapNameTopologyConfig,
					Namespace: namespace,
				}, &updatedJailedConfig)
				assert.NoError(t, err)
				assert.Equal(t, consts.ConfigMapNameTopologyConfig, updatedJailedConfig.Spec.ConfigMap.Name)
				assert.Len(t, updatedJailedConfig.Spec.Items, 1)
				assert.Equal(t, consts.ConfigMapKeyTopologyConfig, updatedJailedConfig.Spec.Items[0].Key)
				assert.Equal(t, filepath.Join("/etc/slurm/", consts.ConfigMapKeyTopologyConfig), updatedJailedConfig.Spec.Items[0].Path)
			}
		})
	}
}
