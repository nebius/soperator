package worker

import (
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderRole(namespace, clusterName string, nodeSet *values.SlurmNodeSet) rbacv1.Role {
	labels := common.RenderLabels(consts.ComponentTypeNodeSet, clusterName)

	ordinals := nodeSetOrdinals(nodeSet)
	resourceNames := make([]string, 0, len(ordinals))
	for _, ordinal := range ordinals {
		resourceNames = append(resourceNames, fmt.Sprintf("%s-%d", nodeSet.StatefulSet.Name, ordinal))
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
				Verbs:         []string{"get", "patch"},
			},
		},
	}
}

func nodeSetOrdinals(nodeSet *values.SlurmNodeSet) []int32 {
	if nodeSet.EphemeralNodes != nil && *nodeSet.EphemeralNodes {
		return append([]int32(nil), nodeSet.ActiveNodes...)
	}

	ordinals := make([]int32, 0, nodeSet.StatefulSet.Replicas)
	for ordinal := int32(0); ordinal < nodeSet.StatefulSet.Replicas; ordinal++ {
		ordinals = append(ordinals, ordinal)
	}
	return ordinals
}
