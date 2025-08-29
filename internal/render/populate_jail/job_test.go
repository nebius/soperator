package populate_jail_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/populate_jail"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderPopulateJailJob_PriorityClass(t *testing.T) {
	tests := []struct {
		name          string
		priorityClass string
		expectedClass string
	}{
		{
			name:          "empty priority class",
			priorityClass: "",
			expectedClass: "",
		},
		{
			name:          "custom priority class",
			priorityClass: "high-priority",
			expectedClass: "high-priority",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace := "test-namespace"
			clusterName := "test-cluster"
			clusterType := consts.ClusterTypeGPU

			nodeFilters := []slurmv1.K8sNodeFilter{
				{
					Name: "test-filter",
				},
			}

			volumeSources := []slurmv1.VolumeSource{
				{
					Name: "test-volume-source",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{},
					},
				},
			}

			populateJail := &values.PopulateJail{
				PopulateJail: slurmv1.PopulateJail{
					K8sNodeFilterName: "test-filter",
					PriorityClass:     tt.priorityClass,
				},
				Name: "test-populate-jail",
				ContainerPopulateJail: values.Container{
					Name: "populate-jail",
					NodeContainer: slurmv1.NodeContainer{
						Image:           "nginx:latest",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Resources: corev1.ResourceList{
							corev1.ResourceMemory:           resource.MustParse("1Gi"),
							corev1.ResourceCPU:              resource.MustParse("100m"),
							corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
						},
					},
				},
				VolumeJail: slurmv1.NodeVolume{
					VolumeSourceName: ptr.To("test-volume-source"),
				},
			}

			result := populate_jail.RenderPopulateJailJob(
				namespace,
				clusterName,
				clusterType,
				nodeFilters,
				volumeSources,
				populateJail,
			)

			// Check PriorityClassName
			assert.Equal(t, tt.expectedClass, result.Spec.Template.Spec.PriorityClassName)
		})
	}
}
