package check_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	. "nebius.ai/slurm-operator/internal/check"
)

func Test_CheckResourceRequests(t *testing.T) {
	tests := []struct {
		name        string
		resources   corev1.ResourceRequirements
		expectError bool
	}{
		{
			name: "Valid memory and CPU requests",
			resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
					corev1.ResourceCPU:    resource.MustParse("1"),
				},
			},
			expectError: false,
		},
		{
			name: "No memory request",
			resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1"),
				},
			},
			expectError: true,
		},
		{
			name: "No CPU request",
			resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			},
			expectError: true,
		},
		{
			name: "Zero memory request",
			resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("0"),
					corev1.ResourceCPU:    resource.MustParse("1"),
				},
			},
			expectError: true,
		},
		{
			name: "Zero CPU request",
			resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
					corev1.ResourceCPU:    resource.MustParse("0"),
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckResourceRequests(tt.resources)
			if (err != nil) != tt.expectError {
				t.Errorf("CheckResourceRequests() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}
