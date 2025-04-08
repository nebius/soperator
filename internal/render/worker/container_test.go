package worker

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
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
		name        string
		container   *values.Container
		features    []slurmv1.WorkerFeature
		wantLimits  corev1.ResourceList
		wantReqs    corev1.ResourceList
		wantEnvVars map[string]string
	}{
		{
			name: "With Resources and features",
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
			features: []slurmv1.WorkerFeature{
				{
					Name:         "f42",
					HostlistExpr: "worker-[0-10]",
				},
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
			wantEnvVars: map[string]string{
				"SLURM_FEATURE_f42": "worker-[0-10]",
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
			got, err := renderContainerSlurmd(
				tt.container,
				nil,
				nil,
				"test-cluster",
				consts.ClusterTypeGPU,
				"v1",
				false,
				"{ \"monitoring\": \"https://my-cloud.com/$INSTANCE_ID/monitoring\" }",
				tt.features,
			)
			if err != nil && tt.wantLimits != nil {
				t.Errorf("renderContainerSlurmd() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(got.Resources.Limits, tt.wantLimits) {
				t.Errorf("renderContainerSlurmd() Limits = %v, want %v", got.Resources.Limits, tt.wantLimits)
			}
			if !reflect.DeepEqual(got.Resources.Requests, tt.wantReqs) {
				t.Errorf("renderContainerSlurmd() Requests = %v, want %v", got.Resources.Requests, tt.wantReqs)
			}
			gotEnvVars := make(map[string]string, len(got.Env))
			for _, env := range got.Env {
				gotEnvVars[env.Name] = env.Value
			}
			for key, value := range tt.wantEnvVars {
				assert.Equal(t, value, gotEnvVars[key], "env var for key %q mismatch", key)
			}
		})
	}
}

func Test_RenderRealMemorySlurmd(t *testing.T) {
	tests := []struct {
		name           string
		container      corev1.ResourceRequirements
		resourceMemory string // Original memory resource as a string (e.g., "512Mi", "1G")
		expectedValue  int64  // Expected memory value in mebibytes
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
			expectedValue:  512,     // Expected value in MiB
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
			expectedValue:  953,  // Expected value in MiB (1G = 1024MB, 1024MB / 1.048576 = 953MiB)
		},
		{
			name: "Valid memory resource - 1000MB",
			container: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1000M"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1000M"),
				},
			},
			resourceMemory: "1000M", // Input memory value
			expectedValue:  953,     // Expected value in MiB (1000MB / 1.048576 = 953MiB)
		},
		{
			name:           "No memory resource",
			container:      corev1.ResourceRequirements{},
			resourceMemory: "", // No memory specified
			expectedValue:  0,  // Expected value in MiB
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function under test
			value := renderRealMemorySlurmd(tt.container)

			// Validate the value
			if value != tt.expectedValue {
				t.Errorf("renderRealMemorySlurmd() = %v, expectedValue %v (from %s)", value, tt.expectedValue, tt.resourceMemory)
			}
		})
	}
}
