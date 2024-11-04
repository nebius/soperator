package common

import (
	corev1 "k8s.io/api/core/v1"
)

func CopyNonCPULimits(resourceList corev1.ResourceList) corev1.ResourceList {
	// Create a copy of all the resources except CPU.
	// This is useful for setting limits on resources other than CPU and prevents throttling of cpu.
	// Also, it required value: Limit must be set for non overcommitable resource
	limits := corev1.ResourceList{}
	for resourceName, quantity := range resourceList {
		if resourceName != corev1.ResourceCPU {
			limits[resourceName] = quantity
		}
	}
	return limits
}
