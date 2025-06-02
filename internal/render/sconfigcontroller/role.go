package sconfigcontroller

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
)

func RenderRole(clusterNamespace, clusterName string) rbacv1.Role {
	labels := common.RenderLabels(consts.ComponentTypeSConfigController, clusterName)

	return rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildRoleSConfigControllerName(clusterName),
			Namespace: clusterNamespace,
			Labels:    labels,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
}
