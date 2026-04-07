package common

import (
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderContainerMunge renders [corev1.Container] for munge
func RenderContainerMunge(container *values.Container, opts ...RenderOption) corev1.Container {

	// Not all resources are guaranteed to be set in the container.Resources field.
	options := renderOptions{
		guaranteed: false,
	}

	for _, opt := range opts {
		opt(&options)
	}

	limits := container.Resources

	if !options.guaranteed {
		// Create a copy of the container's limits and add non-CPU resources from Requests
		limits = CopyNonCPUResources(container.Resources)
	}

	// Since 1.29 is native sidecar support, we can use the native restart policy
	restartPolicy := corev1.ContainerRestartPolicy("Always")

	securityContext := &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{
			Add: []corev1.Capability{
				consts.ContainerSecurityContextCapabilitySysAdmin,
			},
		},
		AppArmorProfile: ParseAppArmorProfile(container.AppArmorProfile),
	}

	return corev1.Container{
		Name:            consts.ContainerNameMunge,
		Image:           container.Image,
		Command:         container.Command,
		Args:            container.Args,
		Env:             container.CustomEnv,
		RestartPolicy:   &restartPolicy,
		ImagePullPolicy: container.ImagePullPolicy,
		VolumeMounts: []corev1.VolumeMount{
			RenderVolumeMountMungeKey(),
			RenderVolumeMountMungeSocket(),
		},
		ReadinessProbe:  container.ReadinessProbe,
		LivenessProbe:   container.LivenessProbe,
		SecurityContext: securityContext,
		Resources: corev1.ResourceRequirements{
			Limits:   limits,
			Requests: container.Resources,
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}
}

// RenderPlaceholderContainerMunge renders [corev1.Container] for munge in sleep mode for DaemonSet
func RenderPlaceholderContainerMunge(container *values.Container) corev1.Container {
	// Since Kubernetes 1.29 has native sidecar support, we can use the native restart policy
	restartPolicy := corev1.ContainerRestartPolicy("Always")

	return corev1.Container{
		Name:            consts.ContainerNameMunge,
		Image:           container.Image,
		Command:         []string{"sleep"},
		Args:            []string{"infinity"},
		RestartPolicy:   &restartPolicy,
		ImagePullPolicy: container.ImagePullPolicy,
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				}}},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}
}
