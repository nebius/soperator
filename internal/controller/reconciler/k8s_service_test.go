package reconciler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestServiceReconciler_patch(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name            string
		existingService *corev1.Service
		desiredService  *corev1.Service
		expectError     bool
	}{
		{
			name: "Patch with annotations",
			existingService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Name: "http",
							Port: 80,
						},
					},
				},
			},
			desiredService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeNodePort,
					Ports: []corev1.ServicePort{
						{
							Name: "http",
							Port: 8080,
						},
						{
							Name: "https",
							Port: 443,
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "Patch without annotations",
			existingService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						"existing": "value",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Name: "http",
							Port: 80,
						},
					},
				},
			},
			desiredService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-namespace",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeNodePort,
					Ports: []corev1.ServicePort{
						{
							Name: "https",
							Port: 443,
						},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			r := &ServiceReconciler{
				Reconciler: &Reconciler{
					Client: fakeClient,
					Scheme: scheme,
				},
			}

			// Execute the patch function
			patch, err := r.patch(tt.existingService, tt.desiredService)

			// Verify results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, patch)

				// Apply the patch to verify it works as expected
				// Create a deep copy of the existing service to apply the patch
				patchedService := tt.existingService.DeepCopy()

				// Check that the patched service has the correct spec type
				assert.Equal(t, tt.desiredService.Spec.Type, patchedService.Spec.Type)

				// Check that the annotations are correctly updated
				if len(tt.desiredService.Annotations) > 0 {
					for k, v := range tt.desiredService.Annotations {
						value, exists := patchedService.Annotations[k]
						assert.True(t, exists, "Annotation %s should exist in patched service", k)
						assert.Equal(t, v, value, "Annotation %s should have correct value", k)
					}
				}

				// Check that the ports are correctly updated
				assert.Len(t, patchedService.Spec.Ports, len(tt.desiredService.Spec.Ports))
				for i, port := range tt.desiredService.Spec.Ports {
					assert.Equal(t, port.Name, patchedService.Spec.Ports[i].Name)
					assert.Equal(t, port.Port, patchedService.Spec.Ports[i].Port)
				}
			}
		})
	}
}
