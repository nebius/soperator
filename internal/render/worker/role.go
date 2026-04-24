package worker

import (
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
)

func RenderRole(namespace, clusterName string, nodeSet *slurmv1alpha1.NodeSet) rbacv1.Role {
	labels := common.RenderLabels(consts.ComponentTypeNodeSet, clusterName)

	resourceNames := make([]string, 0, nodeSet.Spec.Replicas)
	for i := range nodeSet.Spec.Replicas {
		resourceNames = append(resourceNames, fmt.Sprintf("%s-%d", naming.BuildNodeSetStatefulSetName(nodeSet.Name), i))
	}

	return rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildRoleNodeSetName(clusterName, nodeSet.Name),
			Namespace: namespace,
			Labels:    labels,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{""},
				Resources:     []string{"pods"},
				ResourceNames: resourceNames,
				Verbs:         []string{"get", "update"},
			},
		},
	}
}
