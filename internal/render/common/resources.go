package common

import (
	corev1 "k8s.io/api/core/v1"
)

// CopyNonCPUResources returns a copy of corev1.ResourceList but excludes CPU from it.
// This is useful for getting resource limits from resource requests for K8s containers.
// They usually should be identical for everything except CPU, because setting CPU limits may lead to throttling by CFS scheduler in Linux.
func CopyNonCPUResources(resourceList corev1.ResourceList) corev1.ResourceList {
	limits := corev1.ResourceList{}
	for resourceName, quantity := range resourceList {
		if resourceName != corev1.ResourceCPU {
			limits[resourceName] = quantity
		}
	}
	return limits
}

type RenderOption func(*renderOptions)

type renderOptions struct {
	guaranteed bool
}

// GuaranteedPod is a RenderOption that sets the guaranteed flag
// Needed for setting the limits of the container to the same values as the requests.
// This is useful for slurm worker cgroupv2 support.
// It's neccessary for cpuset https://kubernetes.io/docs/tasks/administer-cluster/cpu-management-policies/#static-policy
func GuaranteedPod(guaranteed bool) RenderOption {
	return func(opts *renderOptions) {
		opts.guaranteed = guaranteed
	}
}
