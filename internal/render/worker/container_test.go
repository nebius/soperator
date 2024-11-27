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
				corev1.ResourceCPU:              resource.MustParse("100m"),
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
			wantLimits: nil,
			wantReqs:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderContainerSlurmd(tt.container, nil, "test-cluster", consts.ClusterTypeGPU, "v1")
			if err != nil && tt.wantLimits != nil {
				t.Errorf("renderContainerSlurmd() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(got.Resources.Limits, tt.wantLimits) {
				t.Errorf("renderContainerSlurmd() Limits = %v, want %v", got.Resources.Limits, tt.wantLimits)
			}
			if !reflect.DeepEqual(got.Resources.Requests, tt.wantReqs) {
				t.Errorf("renderContainerSlurmd() Requests = %v, want %v", got.Resources.Requests, tt.wantReqs)
			}
		})
	}
}
func Test_RenderRealMemorySlurmd(t *testing.T) {
	tests := []struct {
		name           string
		container      corev1.ResourceRequirements
		resourceMemory string // Original memory resource as a string (e.g., "512Mi", "1G")
		expectError    bool
	}{
		{
			name: "Valid memory resource - 512Mi",
			container: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			},
			resourceMemory: "512Mi", // Input memory value
			expectError:    false,
		},
		{
			name: "Valid memory resource - 1G",
			container: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1G"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1G"),
				},
			},
			resourceMemory: "1G", // Input memory value
			expectError:    false,
		},
		{
			name:           "No memory resource",
			container:      corev1.ResourceRequirements{},
			resourceMemory: "", // No memory specified
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Dynamically parse and calculate the expected value
			var expectedValue int64
			if tt.resourceMemory != "" {
				// Parse the resource memory value
				quantity := resource.MustParse(tt.resourceMemory)
				memoryInBytes, _ := quantity.AsInt64()
				expectedValue = memoryInBytes / 1_000_000 // Convert bytes to MB
			}

			// Call the function under test
			value := renderRealMemorySlurmd(tt.container)

			// Validate the value
			if !tt.expectError && value != expectedValue {
				t.Errorf("renderRealMemorySlurmd() = %v, expectedValue %v (from %s)", value, expectedValue, tt.resourceMemory)
			}
		})
	}
}
