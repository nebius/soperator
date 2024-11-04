package worker

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderContainerSlurmd(t *testing.T) {
	imageName := "test-image"
	containerName := "test-container"

	tests := []struct {
		name       string
		container  *values.Container
		wantLimits corev1.ResourceList
		wantReqs   corev1.ResourceList
	}{
		{
			name: "With Resources",
			container: &values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Image:           imageName,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Port:            8080,
					Resources: corev1.ResourceList{
						corev1.ResourceMemory:           resource.MustParse("1Gi"),
						corev1.ResourceCPU:              resource.MustParse("100m"),
						corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
					},
				},
				Name: containerName,
			},
			wantLimits: corev1.ResourceList{
				corev1.ResourceMemory:           resource.MustParse("1Gi"),
				corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
			},
			wantReqs: corev1.ResourceList{
				corev1.ResourceMemory:           resource.MustParse("1Gi"),
				corev1.ResourceCPU:              resource.MustParse("100m"),
				corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
			},
		},
		{
			name: "Without Resources",
			container: &values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Image:           imageName,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Port:            8080,
				},
				Name: containerName,
			},
			wantLimits: corev1.ResourceList{},
			wantReqs:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderContainerSlurmd(tt.container, nil, "test-cluster", consts.ClusterTypeGPU, "v1")

			if !reflect.DeepEqual(got.Resources.Limits, tt.wantLimits) {
				t.Errorf("renderContainerSlurmd() Limits = %v, want %v", got.Resources.Limits, tt.wantLimits)
			}
			if !reflect.DeepEqual(got.Resources.Requests, tt.wantReqs) {
				t.Errorf("renderContainerSlurmd() Requests = %v, want %v", got.Resources.Requests, tt.wantReqs)
			}
		})
	}
}
