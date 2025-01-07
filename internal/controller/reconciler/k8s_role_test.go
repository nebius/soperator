package reconciler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/naming"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func Test_GetRole(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	scheme := runtime.NewScheme()
	_ = slurmv1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)

	tests := []struct {
		name       string
		cluster    *slurmv1.SlurmCluster
		existingRB *rbacv1.Role
		expectErr  bool
	}{
		{
			name: "Role exists",
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

			existingRB: &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      naming.BuildRoleWorkerName(defaultNameCluster),
					Namespace: defaultNamespace,
				},
			},
			expectErr: false,
		},
		{
			name: "Role does not exist",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			existingRB: nil,
			expectErr:  false,
		},
		{
			name: "Error getting Role",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			existingRB: nil,
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the fake client
			objs := []runtime.Object{}
			if tt.existingRB != nil {
				objs = append(objs, tt.existingRB)
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			r := &RoleReconciler{
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
			role, err := r.getRole(ctx, tt.cluster)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			switch {
			case tt.existingRB != nil:
				assert.Equal(t, tt.existingRB.Name, role.Name)
				assert.Equal(t, tt.existingRB.Namespace, role.Namespace)
			case role != nil:
				assert.Equal(t, "", role.Name)
				assert.Equal(t, "", role.Namespace)
			default:
				assert.Nil(t, role)
			}
		})
	}
}
