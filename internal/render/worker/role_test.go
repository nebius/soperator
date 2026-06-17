package worker_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderRole_ResourceNames(t *testing.T) {
	tests := []struct {
		name         string
		nodeSet      *values.SlurmNodeSet
		expectedPods []string
	}{
		{
			name: "regular nodeset ordinals",
			nodeSet: &values.SlurmNodeSet{
				Name: "workers",
				StatefulSet: values.StatefulSet{
					Replicas: 3,
				},
			},
			expectedPods: []string{"workers-0", "workers-1", "workers-2"},
		},
		{
			name: "ephemeral sparse active ordinals",
			nodeSet: &values.SlurmNodeSet{
				Name:           "workers",
				EphemeralNodes: ptr.To(true),
				ActiveNodes:    []int32{0, 3, 12},
				StatefulSet: values.StatefulSet{
					Replicas:             3,
					MaxUnavailable:       intstr.FromInt32(1),
					MaxConcurrentStartup: intstr.FromInt32(1),
				},
			},
			expectedPods: []string{"workers-0", "workers-3", "workers-12"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			role := worker.RenderRole("default", "soperator", tt.nodeSet)

			assert.Len(t, role.Rules, 1)
			assert.Equal(t, []string{"get", "patch"}, role.Rules[0].Verbs)
			assert.Equal(t, tt.expectedPods, role.Rules[0].ResourceNames)
			assert.Equal(t, consts.ComponentTypeNodeSet.String(), role.Labels[consts.LabelComponentKey])
		})
	}
}
