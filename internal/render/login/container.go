package login

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// renderContainerSshd renders [corev1.Container] for sshd
func renderContainerSshd(
	clusterType consts.ClusterType,
	container *values.Container,
	jailSubMounts []slurmv1.NodeVolumeJailSubMount,
) corev1.Container {
	volumeMounts := []corev1.VolumeMount{
		common.RenderVolumeMountSlurmConfigs(),
		common.RenderVolumeMountJail(),
		common.RenderVolumeMountMungeSocket(),
		common.RenderVolumeMountSecurityLimits(),
		renderVolumeMountSshdKeys(),
		renderVolumeMountSshConfigs(),
		renderVolumeMountSshRootKeys(),
	}
	volumeMounts = append(volumeMounts, common.RenderVolumeMountsForJailSubMounts(jailSubMounts)...)
	// Create a copy of the container's limits and add non-CPU resources from Requests
	limits := common.CopyNonCPULimits(container.Resources)
	return corev1.Container{
		Name:  consts.ContainerNameSshd,
		Image: container.Image,
		Env: []corev1.EnvVar{
			{
				Name:  "SLURM_CLUSTER_TYPE",
				Value: clusterType.String(),
			},
		},
		ImagePullPolicy: container.ImagePullPolicy,
		Ports: []corev1.ContainerPort{{
			Name:          container.Name,
			ContainerPort: container.Port,
			Protocol:      corev1.ProtocolTCP,
		}},
		VolumeMounts: volumeMounts,
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(container.Port),
				},
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(true),
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
