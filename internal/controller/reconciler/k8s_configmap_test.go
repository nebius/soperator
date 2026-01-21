package reconciler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestConfigMapReconciler_patch(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name              string
		existingConfigMap *corev1.ConfigMap
		desiredConfigMap  *corev1.ConfigMap
	}{
		{
			name: "Patch with identical data - should not update",
			existingConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"key1": "value1",
					"key2": "value2",
					"key3": "value3",
				},
			},
			desiredConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"key1": "value1",
					"key2": "value2",
					"key3": "value3",
				},
			},
		},
		{
			name: "Patch with different data - should update",
			existingConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			},
			desiredConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"key1": "value1",
					"key2": "value2-modified",
				},
			},
		},
		{
			name: "Patch with new keys - should update",
			existingConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"key1": "value1",
				},
			},
			desiredConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			},
		},
		{
			name: "Patch with labels update",
			existingConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
					Labels: map[string]string{
						"existing": "label",
					},
				},
				Data: map[string]string{
					"key1": "value1",
				},
			},
			desiredConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "test-namespace",
					Labels: map[string]string{
						"new": "label",
					},
				},
				Data: map[string]string{
					"key1": "value1",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			r := &ConfigMapReconciler{
				Reconciler: &Reconciler{
					Client: fakeClient,
					Scheme: scheme,
				},
			}

			patch, err := r.patch(tt.existingConfigMap, tt.desiredConfigMap)

			assert.NoError(t, err)
			assert.NotNil(t, patch)

			assert.Equal(t, tt.desiredConfigMap.Data, tt.existingConfigMap.Data,
				"Final data should match desired data")

			for k, v := range tt.desiredConfigMap.Labels {
				assert.Equal(t, v, tt.existingConfigMap.Labels[k],
					"Label %s should be updated correctly", k)
			}
		})
	}
}
