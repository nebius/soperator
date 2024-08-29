package reconciler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func Test_IsControllerOwnerExporter(t *testing.T) {
	defaultNameCluster := "test-cluster"

	cluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultNameCluster,
		},
	}

	t.Run("controller is owner", func(t *testing.T) {
		slurmExporter := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: slurmv1.SlurmClusterKind,
						Name: defaultNameCluster,
					},
				},
			},
		}

		isOwner := isControllerOwnerExporter(slurmExporter, cluster)

		assert.True(t, isOwner)
	})

	t.Run("controller is not owner", func(t *testing.T) {
		slurmExporter := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "OtherKind",
						Name: "other-name",
					},
				},
			},
		}

		isOwner := isControllerOwnerExporter(slurmExporter, cluster)

		assert.False(t, isOwner)
	})
}

func Test_GetExporter(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	scheme := runtime.NewScheme()
	_ = slurmv1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name       string
		cluster    *slurmv1.SlurmCluster
		existingPM *appsv1.Deployment
		expectErr  bool
	}{
		{
			name: "Exporter exists",
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

			existingPM: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			expectErr: false,
		},
		{
			name: "Exporter does not exist",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			existingPM: nil,
			expectErr:  false,
		},
		{
			name: "Error getting Exporter",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			existingPM: nil,
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the fake client
			objs := []runtime.Object{}
			if tt.existingPM != nil {
				objs = append(objs, tt.existingPM)
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			r := &SlurmExporterReconciler{
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
			slurmExporter, err := r.getExporter(ctx, tt.cluster)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			switch {
			case tt.existingPM != nil:
				assert.Equal(t, tt.existingPM.Name, slurmExporter.Name)
				assert.Equal(t, tt.existingPM.Namespace, slurmExporter.Namespace)
			case slurmExporter != nil:
				assert.Equal(t, "", slurmExporter.Name)
				assert.Equal(t, "", slurmExporter.Namespace)
			default:
				assert.Nil(t, slurmExporter)
			}
		})
	}
}

func Test_DeleteExporterOwnedByController(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	scheme := runtime.NewScheme()
	_ = slurmv1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name          string
		cluster       *slurmv1.SlurmCluster
		slurmExporter *appsv1.Deployment
		expectErr     bool
	}{
		{
			name: "Exporter deleted successfully",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			slurmExporter: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			expectErr: false,
		},
		{
			name: "Error deleting Exporter",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			slurmExporter: &appsv1.Deployment{
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
			objs := []runtime.Object{tt.slurmExporter}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			r := &SlurmExporterReconciler{
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
			err := r.deleteExporterOwnedByController(ctx, tt.cluster, tt.slurmExporter)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Verify the pod monitor was deleted
				err = fakeClient.Get(ctx, types.NamespacedName{
					Namespace: tt.slurmExporter.Namespace,
					Name:      tt.slurmExporter.Name,
				}, &appsv1.Deployment{})
				assert.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}
