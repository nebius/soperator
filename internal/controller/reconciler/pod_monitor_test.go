package reconciler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

func Test_GetPodMonitor(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	scheme := runtime.NewScheme()
	_ = slurmv1.AddToScheme(scheme)
	_ = prometheusv1.AddToScheme(scheme)

	tests := []struct {
		name       string
		cluster    *slurmv1.SlurmCluster
		existingPM *prometheusv1.PodMonitor
		expectErr  bool
	}{
		{
			name: "PodMonitor exists",
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

			existingPM: &prometheusv1.PodMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			expectErr: false,
		},
		{
			name: "PodMonitor does not exist",
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
			name: "Error getting PodMonitor",
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

			r := &PodMonitorReconciler{
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
			podMonitor, err := r.getPodMonitor(ctx, tt.cluster)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			switch {
			case tt.existingPM != nil:
				assert.Equal(t, tt.existingPM.Name, podMonitor.Name)
				assert.Equal(t, tt.existingPM.Namespace, podMonitor.Namespace)
			case podMonitor != nil:
				assert.Equal(t, "", podMonitor.Name)
				assert.Equal(t, "", podMonitor.Namespace)
			default:
				assert.Nil(t, podMonitor)
			}
		})
	}
}
