package nodeconfigurator

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
)

// RenderInitContainers renders the initContainers for the DaemonSet
func renderInitContainers() []corev1.Container {
	return []corev1.Container{
		renderContainerNodeSysctl(),
	}
}

// RenderContainers renders the containers for the DaemonSet
func renderContainers(nodeConfigurator slurmv1alpha1.NodeConfiguratorSpec) []corev1.Container {
	if nodeConfigurator.Rebooter.Enabled {
		return []corev1.Container{renderContainerRebooter(nodeConfigurator.Rebooter)}
	}
	return []corev1.Container{renderContainerNodeSysctlSleep(nodeConfigurator.SleepContainer)}
}

// RenderContainerRebooter renders [corev1.Container] for reconciliation of node rebooter
func renderContainerRebooter(rebooter slurmv1alpha1.Rebooter) corev1.Container {
	image := fmt.Sprintf("%s:%s", rebooter.Image.Repository, rebooter.Image.Tag)

	rebooter.Env = append(
		rebooter.Env,
		corev1.EnvVar{
			Name:  consts.RebooterMethodEnv,
			Value: rebooter.EvictionMethod,
		}, corev1.EnvVar{
			Name: consts.RebooterNodeNameEnv,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
	)

	return corev1.Container{
		Name:            consts.ContainerNameRebooter,
		Image:           image,
		ImagePullPolicy: rebooter.Image.PullPolicy,
		SecurityContext: createSecurityContext(true, 0, 0, true, true, nil),
		Command: []string{
			"/usr/bin/rebooter",
		},
		Args: []string{
			"-log-level=" + rebooter.LogLevel,
			"-log-format=" + rebooter.LogFormat,
		},
		Resources: createResourceRequirements(
			*rebooter.Resources.Limits.Cpu(),
			*rebooter.Resources.Requests.Cpu(),
			*rebooter.Resources.Requests.Memory(),
		),
		Env:            rebooter.Env,
		ReadinessProbe: rebooter.ReadinessProbe,
		LivenessProbe:  rebooter.LivenessProbe,
	}
}

// RenderContainerNodeSysctlSleep renders [corev1.Container] for reconciliation of sysctl
func renderContainerNodeSysctlSleep(sleepContainer slurmv1alpha1.SleepContainer) corev1.Container {
	if sleepContainer.Image.Repository == "" {
		sleepContainer.Image.Repository = "busybox"
	}
	if sleepContainer.Image.Tag == "" {
		sleepContainer.Image.Tag = "latest"
	}
	return corev1.Container{
		Name:            consts.ContainerNameNodeSysctlSleep,
		Image:           fmt.Sprintf("%s:%s", sleepContainer.Image.Repository, sleepContainer.Image.Tag),
		SecurityContext: createSecurityContext(false, 65534, 65534, true, false, []corev1.Capability{"ALL"}),
		Resources: createResourceRequirements(
			*sleepContainer.Resources.Limits.Memory(),
			*sleepContainer.Resources.Requests.Cpu(),
			*sleepContainer.Resources.Requests.Memory(),
		),
		Command: []string{
			"/bin/sh",
			"-c",
			"sleep infinity",
		},
		Env:            sleepContainer.Env,
		ReadinessProbe: sleepContainer.ReadinessProbe,
		LivenessProbe:  sleepContainer.LivenessProbe,
	}
}

// Common function to create a SecurityContext
func createSecurityContext(privileged bool, runAsUser, runAsGroup int64, readOnlyRootFilesystem, allowPrivilegeEscalation bool, dropCapabilities []corev1.Capability) *corev1.SecurityContext {
	if privileged {
		// Cannot set `allowPrivilegeEscalation` to false and `privileged` to true"
		allowPrivilegeEscalation = true
	}
	return &corev1.SecurityContext{
		Privileged:               ptr.To(privileged),
		RunAsUser:                ptr.To(runAsUser),
		RunAsGroup:               ptr.To(runAsGroup),
		RunAsNonRoot:             ptr.To(!privileged),
		ReadOnlyRootFilesystem:   ptr.To(readOnlyRootFilesystem),
		AllowPrivilegeEscalation: ptr.To(allowPrivilegeEscalation),
		Capabilities: &corev1.Capabilities{
			Drop: dropCapabilities,
		},
	}
}

// Common function to create ResourceRequirements
func createResourceRequirements(limitsMemory, requestsCPU, requestsMemory resource.Quantity) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: limitsMemory,
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    requestsCPU,
			corev1.ResourceMemory: requestsMemory,
		},
	}
}

// RenderContainerNodeSysctl renders [corev1.Container] for modify k8s node sysctl
func renderContainerNodeSysctl() corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameNodeSysctl,
		Image:           "busybox:latest",
		SecurityContext: createSecurityContext(true, 0, 0, false, true, nil),
		Resources: createResourceRequirements(
			resource.MustParse("8Mi"),
			resource.MustParse("10m"),
			resource.MustParse("8Mi"),
		),
		Command: []string{
			"/bin/sh",
			"-c",
			"sysctl -w kernel.unprivileged_userns_clone=1",
		},
	}
}
