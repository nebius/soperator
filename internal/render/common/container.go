package common

import (
	"fmt"
	"path"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderContainerMunge renders [corev1.Container] for munge
func RenderContainerMunge(container *values.Container) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameMunge,
		Image:           container.Image,
		ImagePullPolicy: corev1.PullAlways, // TODO use digest and set to corev1.PullIfNotPresent
		VolumeMounts: []corev1.VolumeMount{
			RenderVolumeMountMungeKey(),
			RenderVolumeMountJail(),
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
		},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				}}},
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:              container.Resources.CPU,
				corev1.ResourceMemory:           container.Resources.Memory,
				corev1.ResourceEphemeralStorage: container.Resources.EphemeralStorage,
			},
		},
	}
}
