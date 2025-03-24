package controller

import (
	corev1 "k8s.io/api/core/v1"
	slurmv1 "nebius.ai/slurm-operator/api/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// renderContainerSlurmctld renders [corev1.Container] for slurmctld
func renderContainerSlurmctld(container *values.Container, customMounts []slurmv1.NodeVolumeMount) corev1.Container {
	volumeMounts := []corev1.VolumeMount{
		common.RenderVolumeMountSpool(consts.ComponentTypeController, consts.SlurmctldName),
		common.RenderVolumeMountJail(),
		common.RenderVolumeMountMungeSocket(),
		common.RenderVolumeMountSecurityLimits(),
		common.RenderVolumeMountRESTJWTKey(),
	}
	volumeMounts = append(volumeMounts, common.RenderVolumeMounts(customMounts, "")...)

	// Create a copy of the container's limits and add non-CPU resources from Requests
	limits := common.CopyNonCPUResources(container.Resources)
	return corev1.Container{
		Name:            consts.ContainerNameSlurmctld,
		Image:           container.Image,
		ImagePullPolicy: container.ImagePullPolicy,
		Ports: []corev1.ContainerPort{{
			Name:          container.Name,
			ContainerPort: container.Port,
			Protocol:      corev1.ProtocolTCP,
		}},
		VolumeMounts: volumeMounts,
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"scontrol",
						"ping",
					},
				},
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				},
			},
		},
		Resources: corev1.ResourceRequirements{
			Limits:   limits,
			Requests: container.Resources,
		},
	}
}
