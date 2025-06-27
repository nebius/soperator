package exporter

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
)

func RenderExporterRoleBinding(clusterNamespace, clusterName string) rbacv1.RoleBinding {
	labels := common.RenderLabels(consts.ComponentTypeExporter, clusterName)

	return rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      RoleBindingName,
			Namespace: clusterNamespace,
			Labels:    labels,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      ServiceAccountName,
				Namespace: clusterNamespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     RoleName,
			APIGroup: rbacv1.GroupName,
		},
	}
}
