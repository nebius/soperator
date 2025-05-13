package soperatorchecks

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
)

func RenderRole(namespace, clusterName string) rbacv1.Role {
	labels := common.RenderLabels(consts.ComponentTypeSoperatorChecks, clusterName)

	return rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildRoleActiveCheckName(clusterName),
			Namespace: namespace,
			Labels:    labels,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get"},
			},
			{
				APIGroups: []string{"batch"},
				Resources: []string{"jobs"},
				Verbs:     []string{"get", "patch"},
			},
		},
	}
}
