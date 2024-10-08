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

func Test_IsControllerOwnerRoleBinding(t *testing.T) {
	defaultNameCluster := "test-cluster"

	cluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultNameCluster,
		},
	}

	t.Run("controller is owner", func(t *testing.T) {
		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: slurmv1.SlurmClusterKind,
						Name: defaultNameCluster,
					},
				},
			},
		}

		isOwner := isControllerOwnerRoleBinding(roleBinding, cluster)

		assert.True(t, isOwner)
	})

	t.Run("controller is not owner", func(t *testing.T) {
		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "OtherKind",
						Name: "other-name",
					},
				},
			},
		}

		isOwner := isControllerOwnerRoleBinding(roleBinding, cluster)

		assert.False(t, isOwner)
	})
}

func Test_GetRoleBinding(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	scheme := runtime.NewScheme()
	_ = slurmv1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)

	tests := []struct {
		name       string
		cluster    *slurmv1.SlurmCluster
		existingRB *rbacv1.RoleBinding
		expectErr  bool
	}{
		{
			name: "RoleBinding exists",
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

			existingRB: &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      naming.BuildRoleBindingWorkerName(defaultNameCluster),
					Namespace: defaultNamespace,
				},
			},
			expectErr: false,
		},
		{
			name: "RoleBinding does not exist",
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
			name: "Error getting RoleBinding",
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

			r := &RoleBindingReconciler{
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
			roleBinding, err := r.getRoleBinding(ctx, tt.cluster)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			switch {
			case tt.existingRB != nil:
				assert.Equal(t, tt.existingRB.Name, roleBinding.Name)
				assert.Equal(t, tt.existingRB.Namespace, roleBinding.Namespace)
			case roleBinding != nil:
				assert.Equal(t, "", roleBinding.Name)
				assert.Equal(t, "", roleBinding.Namespace)
			default:
				assert.Nil(t, roleBinding)
			}
		})
	}
}

func Test_DeleteRoleBindingOwnedByController(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	scheme := runtime.NewScheme()
	_ = slurmv1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)

	tests := []struct {
		name        string
		cluster     *slurmv1.SlurmCluster
		roleBinding *rbacv1.RoleBinding
		expectErr   bool
	}{
		{
			name: "RoleBinding deleted successfully",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			roleBinding: &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      naming.BuildRoleBindingWorkerName(defaultNameCluster),
					Namespace: defaultNamespace,
				},
			},
			expectErr: false,
		},
		{
			name: "Error deleting RoleBinding",
			cluster: &slurmv1.SlurmCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNameCluster,
					Namespace: defaultNamespace,
				},
			},
			roleBinding: &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      naming.BuildRoleBindingWorkerName(defaultNameCluster),
					Namespace: defaultNamespace,
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the fake client
			objs := []runtime.Object{tt.roleBinding}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			r := &RoleBindingReconciler{
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
			err := r.deleteRoleBindingOwnedByController(ctx, tt.cluster, tt.roleBinding)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Verify the role binding was deleted
				err = fakeClient.Get(ctx, types.NamespacedName{
					Namespace: tt.roleBinding.Namespace,
					Name:      tt.roleBinding.Name,
				}, &rbacv1.RoleBinding{})
				assert.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}
