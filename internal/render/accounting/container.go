package accounting

import (
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// renderContainerAccounting renders [corev1.Container] for slurmctld
func renderContainerAccounting(container values.Container) corev1.Container {
	if container.Port == 0 {
		container.Port = consts.DefaultAccountingPort
	}
	container.NodeContainer.Resources.Storage()
	return corev1.Container{
		Name:            consts.ContainerNameAccounting,
		Image:           container.Image,
		ImagePullPolicy: corev1.PullAlways, // TODO use digest and set to corev1.PullIfNotPresent
		Ports: []corev1.ContainerPort{{
			Name:          container.Name,
			ContainerPort: container.Port,
			Protocol:      corev1.ProtocolTCP,
		}},
		VolumeMounts: []corev1.VolumeMount{
			common.RenderVolumeMountSlurmConfigs(),
			common.RenderVolumeMountMungeSocket(),
			RenderVolumeMountSlurmdbdConfigs(),
			RenderVolumeMountSlurmdbdSpool(),
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/sh",
						"-c",
						"/usr/bin/sacct > /dev/null && exit 0 || exit 1",
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
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: *container.Resources.Memory(),
			},
			Requests: container.Resources,
		},
	}
}
