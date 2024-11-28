package check

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

func CheckResourceRequests(resources corev1.ResourceRequirements) error {
	memoryQuantity, memoryOk := resources.Requests[corev1.ResourceMemory]
	cpuQuantity, cpuOk := resources.Requests[corev1.ResourceCPU]

	if !memoryOk || memoryQuantity.IsZero() {
		return fmt.Errorf("memory request not set or is zero")
	}

	if !cpuOk || cpuQuantity.IsZero() {
		return fmt.Errorf("CPU request not set or is zero")
	}

	return nil
}
