package reconciler

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

func TestServiceReconciler_patch(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	var tests = []struct {
		name                string
		existingService     *corev1.Service
		desiredService      *corev1.Service
		expectedAnnotations map[string]string
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
			expectedAnnotations: map[string]string{"key1": "value1", "key2": "value2"},
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
			expectedAnnotations: map[string]string{"existing": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingService.DeepCopy()).Build()
			r := &ServiceReconciler{
				Reconciler: &Reconciler{
					Client: fakeClient,
					Scheme: scheme,
				},
			}

			patch, err := r.patch(tt.existingService, tt.desiredService)
			require.NoError(t, err)
			require.NotNil(t, patch)
			require.NoError(t, fakeClient.Patch(t.Context(), tt.existingService, patch))

			patchedService := &corev1.Service{}
			require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(tt.existingService), patchedService))

			assert.Equal(t, tt.desiredService.Spec.Type, patchedService.Spec.Type)
			assert.Equal(t, tt.desiredService.Spec.Ports, patchedService.Spec.Ports)
			assert.Equal(t, tt.expectedAnnotations, patchedService.Annotations)
		})
	}
}

func TestServiceReconciler_ReconcileLoadBalancer(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, slurmv1.AddToScheme(scheme))

	var tests = []struct {
		name           string
		existingRanges []string
		desiredRanges  []string
		existingIP     string
		desiredIP      string
	}{
		{
			name:          "Add source ranges",
			desiredRanges: []string{"213.213.213.54/32"},
		},
		{
			name:           "Replace source ranges and preserve requested IP",
			existingRanges: []string{"10.0.0.0/8", "172.16.0.0/12"},
			desiredRanges:  []string{"213.213.213.54/32"},
			existingIP:     "192.0.2.10",
			desiredIP:      "192.0.2.10",
		},
		{
			name:           "Clear source ranges with nil",
			existingRanges: []string{"10.0.0.0/8"},
		},
		{
			name:           "Clear source ranges with empty slice",
			existingRanges: []string{"10.0.0.0/8"},
			desiredRanges:  []string{},
		},
		{
			name:      "Set requested IP",
			desiredIP: "192.0.2.10",
		},
		{
			name:           "Change requested IP and preserve source ranges",
			existingRanges: []string{"213.213.213.54/32"},
			desiredRanges:  []string{"213.213.213.54/32"},
			existingIP:     "192.0.2.10",
			desiredIP:      "192.0.2.20",
		},
		{
			name:       "Clear requested IP",
			existingIP: "192.0.2.10",
		},
		{
			name:           "Update source ranges and requested IP",
			existingRanges: []string{"10.0.0.0/8"},
			desiredRanges:  []string{"213.213.213.54/32", "192.0.2.0/24"},
			existingIP:     "192.0.2.10",
			desiredIP:      "192.0.2.20",
		},
		{
			name:           "Preserve unchanged settings",
			existingRanges: []string{"213.213.213.54/32"},
			desiredRanges:  []string{"213.213.213.54/32"},
			existingIP:     "192.0.2.10",
			desiredIP:      "192.0.2.10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := &slurmv1.SlurmCluster{ObjectMeta: metav1.ObjectMeta{
				Name: "test-cluster", Namespace: "test-namespace", UID: "cluster-uid",
			}}
			existing := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-service",
					Namespace:       owner.Namespace,
					UID:             "service-uid",
					Annotations:     map[string]string{"provider": "preserved"},
					OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(owner, slurmv1.GroupVersion.WithKind("SlurmCluster"))},
				},
				Spec: corev1.ServiceSpec{
					Type:                     corev1.ServiceTypeLoadBalancer,
					Ports:                    []corev1.ServicePort{{Name: "ssh", Port: 22, NodePort: 30022, Protocol: corev1.ProtocolTCP}},
					ClusterIP:                "10.96.0.1",
					ClusterIPs:               []string{"10.96.0.1"},
					Selector:                 map[string]string{"app": "login"},
					LoadBalancerSourceRanges: tt.existingRanges,
					LoadBalancerIP:           tt.existingIP,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "192.0.2.10"}}},
				},
			}
			desired := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: existing.Name, Namespace: existing.Namespace},
				Spec: corev1.ServiceSpec{
					Type:                     corev1.ServiceTypeLoadBalancer,
					Ports:                    slices.Clone(existing.Spec.Ports),
					LoadBalancerSourceRanges: tt.desiredRanges,
					LoadBalancerIP:           tt.desiredIP,
				},
			}
			var creates, deletes, patches int
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(existing).WithStatusSubresource(&corev1.Service{}).
				WithInterceptorFuncs(interceptor.Funcs{
					Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						creates++
						return c.Create(ctx, obj, opts...)
					},
					Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
						deletes++
						return c.Delete(ctx, obj, opts...)
					},
					Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						patches++
						if patches == 2 {
							data, err := patch.Data(obj)
							require.NoError(t, err)
							assert.JSONEq(t, `{}`, string(data))
						}
						return c.Patch(ctx, obj, patch, opts...)
					},
				}).Build()
			r := NewServiceReconciler(NewReconciler(fakeClient, scheme, nil))
			expectedSpec := existing.Spec.DeepCopy()
			expectedSpec.LoadBalancerSourceRanges = nil
			if len(tt.desiredRanges) > 0 {
				expectedSpec.LoadBalancerSourceRanges = tt.desiredRanges
			}
			expectedSpec.LoadBalancerIP = tt.desiredIP

			for range 2 {
				require.NoError(t, r.Reconcile(t.Context(), owner, desired.DeepCopy(), nil))
				current := &corev1.Service{}
				require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(existing), current))
				assert.Equal(t, *expectedSpec, current.Spec)
				assert.Equal(t, existing.UID, current.UID)
				assert.Equal(t, existing.Annotations, current.Annotations)
				assert.Equal(t, existing.OwnerReferences, current.OwnerReferences)
				assert.Equal(t, existing.Status, current.Status)
			}
			assert.Zero(t, creates)
			assert.Zero(t, deletes)
			assert.Equal(t, 2, patches)
		})
	}
}
