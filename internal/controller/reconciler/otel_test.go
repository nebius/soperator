package reconciler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	otelv1beta1 "github.com/open-telemetry/opentelemetry-operator/apis/v1beta1"
)

func Test_IsControllerOwnerOtel(t *testing.T) {
	defaultNameCluster := "test-cluster"

	cluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultNameCluster,
		},
	}

	t.Run("controller is owner", func(t *testing.T) {
		otel := &otelv1beta1.OpenTelemetryCollector{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: slurmv1.SlurmClusterKind,
						Name: defaultNameCluster,
					},
				},
			},
		}

		isOwner := isControllerOwnerOtel(otel, cluster)

		assert.True(t, isOwner)
	})

	t.Run("controller is not owner", func(t *testing.T) {
		otel := &otelv1beta1.OpenTelemetryCollector{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "OtherKind",
						Name: "other-name",
					},
				},
			},
		}

		isOwner := isControllerOwnerOtel(otel, cluster)

		assert.False(t, isOwner)
	})
}

func Test_GetOtel(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	scheme := runtime.NewScheme()
	_ = slurmv1.AddToScheme(scheme)
	_ = otelv1beta1.AddToScheme(scheme)

	tests := []struct {
		name         string
		cluster      *slurmv1.SlurmCluster
		existingOtel *otelv1beta1.OpenTelemetryCollector
		expectErr    bool
	}{
		{
			name: "Otel exists",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
				Spec: slurmv1.SlurmClusterSpec{
					Telemetry: &slurmv1.Telemetry{
						JobsTelemetry: &slurmv1.JobsTelemetry{
							SendJobsEvents: true,
						},
					},
				},
			},

			existingOtel: &otelv1beta1.OpenTelemetryCollector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			expectErr: false,
		},
		{
			name: "Otel does not exist",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			existingOtel: nil,
			expectErr:    false,
		},
		{
			name: "Error getting Otel",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			existingOtel: nil,
			expectErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the fake client
			objs := []runtime.Object{}
			if tt.existingOtel != nil {
				objs = append(objs, tt.existingOtel)
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			r := &OtelReconciler{
				Reconciler: &Reconciler{
					Client: fakeClient,
					Scheme: scheme,
				},
			}

			if tt.expectErr {
				// Override the client with our fake Gone client to simulate the "IsGone" error
				r.Client = &fakeGoneClient{Client: fakeClient}
			}

			// Run the test
			ctx := context.TODO()
			otel, err := r.getOtel(ctx, tt.cluster)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.existingOtel != nil {
				assert.Equal(t, tt.existingOtel.Name, otel.Name)
				assert.Equal(t, tt.existingOtel.Namespace, otel.Namespace)
			} else if otel != nil {
				assert.Equal(t, "", otel.Name)
				assert.Equal(t, "", otel.Namespace)
			} else {
				assert.Nil(t, otel)
			}
		})
	}
}

func Test_DeleteOtelOwnedByController(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	scheme := runtime.NewScheme()
	_ = slurmv1.AddToScheme(scheme)
	_ = otelv1beta1.AddToScheme(scheme)

	tests := []struct {
		name      string
		cluster   *slurmv1.SlurmCluster
		otel      *otelv1beta1.OpenTelemetryCollector
		expectErr bool
	}{
		{
			name: "Otel deleted successfully",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			otel: &otelv1beta1.OpenTelemetryCollector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			expectErr: false,
		},
		{
			name: "Error deleting Otel",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			otel: &otelv1beta1.OpenTelemetryCollector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the fake client
			objs := []runtime.Object{tt.otel}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			r := &OtelReconciler{
				Reconciler: &Reconciler{
					Client: fakeClient,
					Scheme: scheme,
				},
			}

			if tt.expectErr {
				// Override the client with our fake client to simulate the error on delete
				r.Client = &fakeErrorClient{Client: fakeClient}
			}

			// Run the test
			ctx := context.TODO()
			err := r.deleteOtelOwnedByController(ctx, tt.cluster, tt.otel)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Verify the otel collector was deleted
				err = fakeClient.Get(ctx, types.NamespacedName{
					Namespace: tt.otel.Namespace,
					Name:      tt.otel.Name,
				}, &otelv1beta1.OpenTelemetryCollector{})
				assert.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}
