package sconfigcontroller_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/render/sconfigcontroller"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_BasePodTemplateSpec_PriorityClass(t *testing.T) {
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
			clusterNamespace := "test-namespace"
			clusterName := "test-cluster"
			slurmAPIServer := "http://slurm-api-server"

			sConfigController := &values.SConfigController{
				SlurmNode: slurmv1.SlurmNode{
					Size:              1,
					K8sNodeFilterName: "test-filter",
					PriorityClass:     tt.priorityClass,
				},
				Container: values.Container{
					Name: "test-container",
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

			matchLabels := map[string]string{"app": "test"}

			result, err := sconfigcontroller.BasePodTemplateSpec(
				clusterNamespace,
				clusterName,
				slurmAPIServer,
				sConfigController,
				nodeFilters,
				volumeSources,
				matchLabels,
			)

			assert.NoError(t, err)

			// Check PriorityClassName
			assert.Equal(t, tt.expectedClass, result.Spec.PriorityClassName)
		})
	}
}
