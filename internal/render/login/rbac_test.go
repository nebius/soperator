package login

import (
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderLoginRBAC(t *testing.T) {
	tests := []struct {
		name         string
		replicas     int32
		autoscaling  *slurmv1.LoginAutoscaling
		expectedPods []string
	}{
		{
			name: "fixed replicas", replicas: 2,
			expectedPods: []string{"custom-login-0", "custom-login-1"},
		},
		{
			name: "autoscaling maximum", replicas: 1,
			autoscaling:  &slurmv1.LoginAutoscaling{Enabled: true, MaxReplicas: 3},
			expectedPods: []string{"custom-login-0", "custom-login-1", "custom-login-2"},
		},
		{
			name: "disabled autoscaling", replicas: 1,
			autoscaling:  &slurmv1.LoginAutoscaling{Enabled: false, MaxReplicas: 3},
			expectedPods: []string{"custom-login-0"},
		},
		{
			name: "fixed count exceeds autoscaling maximum", replicas: 2,
			autoscaling:  &slurmv1.LoginAutoscaling{Enabled: true, MaxReplicas: 1},
			expectedPods: []string{"custom-login-0", "custom-login-1"},
		},
		{name: "zero replicas grants no pod access"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const namespace, clusterName = "default", "test-cluster"
			login := &values.SlurmLogin{
				StatefulSet: values.StatefulSet{Name: "custom-login", Replicas: tt.replicas},
				Autoscaling: tt.autoscaling,
			}
			account := RenderServiceAccount(namespace, clusterName)
			role := RenderRole(namespace, clusterName, login)
			binding := RenderRoleBinding(namespace, clusterName)
			require.Equal(t, naming.BuildServiceAccountLoginName(clusterName), account.Name)
			require.Equal(t, namespace, account.Namespace)
			require.Equal(t, consts.ComponentTypeLogin.String(), account.Labels[consts.LabelComponentKey])
			require.Equal(t, naming.BuildRoleLoginName(clusterName), role.Name)
			require.Equal(t, namespace, role.Namespace)
			if len(tt.expectedPods) == 0 {
				require.Empty(t, role.Rules)
			} else {
				require.Equal(t, []rbacv1.PolicyRule{{
					APIGroups: []string{""}, Resources: []string{"pods"},
					Verbs: []string{"get", "patch"}, ResourceNames: tt.expectedPods,
				}}, role.Rules)
			}
			require.Equal(t, naming.BuildRoleBindingLoginName(clusterName), binding.Name)
			require.Equal(t, namespace, binding.Namespace)
			require.Equal(t, []rbacv1.Subject{{
				Kind: rbacv1.ServiceAccountKind, Name: account.Name, Namespace: namespace,
			}}, binding.Subjects)
			require.Equal(t, rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name}, binding.RoleRef)
		})
	}
}
