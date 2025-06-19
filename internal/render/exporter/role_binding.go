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
			Name:      BuildExporterRoleBindingName(clusterName),
			Namespace: clusterNamespace,
			Labels:    labels,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      BuildExporterServiceAccountName(clusterName),
				Namespace: clusterNamespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     BuildExporterRoleName(clusterName),
			APIGroup: rbacv1.GroupName,
		},
	}
}
