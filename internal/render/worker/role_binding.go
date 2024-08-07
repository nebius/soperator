package worker

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
)

func RenderRoleBinding(namespace, clusterName string) rbacv1.RoleBinding {
	labels := common.RenderLabels(consts.ComponentTypeWorker, clusterName)

	return rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildRoleBindingWorkerName(clusterName),
			Namespace: namespace,
			Labels:    labels,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      naming.BuildServiceAccountWorkerName(clusterName),
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     naming.BuildRoleWorkerName(clusterName),
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
}
