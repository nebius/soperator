package login

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderServiceAccount(namespace, clusterName string) corev1.ServiceAccount {
	return corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildServiceAccountLoginName(clusterName),
			Namespace: namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeLogin, clusterName),
		},
	}
}

func RenderRole(namespace, clusterName string, login *values.SlurmLogin) rbacv1.Role {
	role := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildRoleLoginName(clusterName),
			Namespace: namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeLogin, clusterName),
		},
	}
	replicas := login.StatefulSet.Replicas
	if login.IsAutoscalingEnabled() {
		replicas = max(replicas, login.Autoscaling.MaxReplicas)
	}
	if replicas <= 0 {
		// An empty resourceNames list would grant access to every pod in the namespace.
		return role
	}

	var resourceNames []string
	for ordinal := int32(0); ordinal < replicas; ordinal++ {
		resourceNames = append(resourceNames, fmt.Sprintf("%s-%d", login.StatefulSet.Name, ordinal))
	}
	role.Rules = []rbacv1.PolicyRule{{
		APIGroups:     []string{""},
		Resources:     []string{"pods"},
		ResourceNames: resourceNames,
		Verbs:         []string{"get", "patch"},
	}}
	return role
}

func RenderRoleBinding(namespace, clusterName string) rbacv1.RoleBinding {
	return rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildRoleBindingLoginName(clusterName),
			Namespace: namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeLogin, clusterName),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      naming.BuildServiceAccountLoginName(clusterName),
			Namespace: namespace,
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     naming.BuildRoleLoginName(clusterName),
		},
	}
}
