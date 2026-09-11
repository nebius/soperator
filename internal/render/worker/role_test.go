package worker_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderNodeSetRBAC(t *testing.T) {
	tests := []struct {
		name         string
		nodeSet      *values.SlurmNodeSet
		expectedPods []string
	}{
		{
			name: "regular NodeSet ordinals",
			nodeSet: &values.SlurmNodeSet{
				Name: "workers",
				StatefulSet: values.StatefulSet{
					Name:     "workers",
					Replicas: 3,
				},
			},
			expectedPods: []string{"workers-0", "workers-1", "workers-2"},
		},
		{
			name: "ephemeral NodeSet active ordinals",
			nodeSet: &values.SlurmNodeSet{
				Name:           "workers",
				EphemeralNodes: ptr.To(true),
				ActiveNodes:    []int32{0, 3, 12},
				StatefulSet: values.StatefulSet{
					Name:     "workers",
					Replicas: 3,
				},
			},
			expectedPods: []string{"workers-0", "workers-3", "workers-12"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const namespace = "default"
			const clusterName = "soperator"

			serviceAccount := worker.RenderServiceAccount(namespace, clusterName, tt.nodeSet.Name)
			role := worker.RenderRole(namespace, clusterName, tt.nodeSet)
			roleBinding := worker.RenderRoleBinding(namespace, clusterName, tt.nodeSet.Name)

			expectedServiceAccountName := naming.BuildServiceAccountNodeSetName(clusterName, tt.nodeSet.Name)
			expectedRoleName := naming.BuildRoleNodeSetName(clusterName, tt.nodeSet.Name)

			assert.Equal(t, expectedServiceAccountName, serviceAccount.Name)
			assert.Equal(t, consts.ComponentTypeNodeSet.String(), serviceAccount.Labels[consts.LabelComponentKey])

			assert.Equal(t, expectedRoleName, role.Name)
			assert.Len(t, role.Rules, 1)
			assert.Equal(t, []string{"pods"}, role.Rules[0].Resources)
			assert.Equal(t, []string{"get", "patch"}, role.Rules[0].Verbs)
			assert.Equal(t, tt.expectedPods, role.Rules[0].ResourceNames)

			assert.Equal(t, naming.BuildRoleBindingNodeSetName(clusterName, tt.nodeSet.Name), roleBinding.Name)
			assert.Len(t, roleBinding.Subjects, 1)
			assert.Equal(t, expectedServiceAccountName, roleBinding.Subjects[0].Name)
			assert.Equal(t, namespace, roleBinding.Subjects[0].Namespace)
			assert.Equal(t, expectedRoleName, roleBinding.RoleRef.Name)
		})
	}
}
