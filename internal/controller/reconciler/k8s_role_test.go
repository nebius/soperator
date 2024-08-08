package reconciler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/naming"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func Test_IsControllerOwnerRole(t *testing.T) {
	cluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultNameCluster,
		},
	}

	t.Run("controller is owner", func(t *testing.T) {
		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: slurmv1.SlurmClusterKind,
						Name: defaultNameCluster,
					},
				},
			},
		}

		isOwner := isControllerOwnerRole(role, cluster)

		assert.True(t, isOwner)
	})

	t.Run("controller is not owner", func(t *testing.T) {
		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "OtherKind",
						Name: "other-name",
					},
				},
			},
		}

		isOwner := isControllerOwnerRole(role, cluster)

		assert.False(t, isOwner)
	})
}

func Test_GetRole(t *testing.T) {
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
					PeriodicChecks: slurmv1.PeriodicChecks{
						NCCLBenchmark: slurmv1.NCCLBenchmark{
							SendJobsEvents: &[]bool{true}[0],
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

			if tt.existingRB != nil {
				assert.Equal(t, tt.existingRB.Name, role.Name)
				assert.Equal(t, tt.existingRB.Namespace, role.Namespace)
			} else {
				assert.Equal(t, "", role.Name)
				assert.Equal(t, "", role.Namespace)
			}
		})
	}
}

func Test_DeleteRoleOwnedByController(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = slurmv1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)

	tests := []struct {
		name      string
		cluster   *slurmv1.SlurmCluster
		role      *rbacv1.Role
		expectErr bool
	}{
		{
			name: "Role deleted successfully",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			role: &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      naming.BuildRoleWorkerName(defaultNameCluster),
					Namespace: defaultNamespace,
				},
			},
			expectErr: false,
		},
		{
			name: "Error deleting Role",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			role: &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      naming.BuildRoleWorkerName(defaultNameCluster),
					Namespace: defaultNamespace,
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the fake client
			objs := []runtime.Object{tt.role}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			r := &RoleReconciler{
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
			err := r.deleteRoleOwnedByController(ctx, tt.cluster, tt.role)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Verify the role binding was deleted
				err = fakeClient.Get(ctx, types.NamespacedName{
					Namespace: tt.role.Namespace,
					Name:      tt.role.Name,
				}, &rbacv1.Role{})
				assert.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}
