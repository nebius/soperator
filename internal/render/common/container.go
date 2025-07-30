package common

import (
	"fmt"
	"path"

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

	return corev1.Container{
		Name:            consts.ContainerNameMunge,
		Image:           container.Image,
		Command:         container.Command,
		Args:            container.Args,
		RestartPolicy:   &restartPolicy,
		ImagePullPolicy: container.ImagePullPolicy,
		VolumeMounts: []corev1.VolumeMount{
			RenderVolumeMountMungeKey(),
			RenderVolumeMountMungeSocket(),
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/sh",
						"-c",
						fmt.Sprintf(
							"test -S %s",
							path.Join(consts.VolumeMountPathMungeSocket, "munge.socket.2"),
						),
					},
				},
			},
			TimeoutSeconds:   DefaultProbeTimeoutSeconds,
			PeriodSeconds:    DefaultProbePeriodSeconds,
			SuccessThreshold: DefaultProbeSuccessThreshold,
			FailureThreshold: DefaultProbeFailureThreshold,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/sh",
						"-c",
						"/usr/bin/munge -n > /dev/null && exit 0 || exit 1",
					},
				},
			},
			TimeoutSeconds:   DefaultProbeTimeoutSeconds,
			PeriodSeconds:    DefaultProbePeriodSeconds,
			SuccessThreshold: DefaultProbeSuccessThreshold,
			FailureThreshold: DefaultProbeFailureThreshold,
		},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				}}},
		Resources: corev1.ResourceRequirements{
			Limits:   limits,
			Requests: container.Resources,
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}
}

// RenderContainerMungeSleep renders [corev1.Container] for munge in sleep mode for DaemonSet
func RenderContainerMungeSleep(container *values.Container) corev1.Container {
	// Since 1.29 is native sidecar support, we can use the native restart policy
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
