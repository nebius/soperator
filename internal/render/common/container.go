package common

import (
	"fmt"
	"path"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
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
		RestartPolicy:   &restartPolicy,
		ImagePullPolicy: container.ImagePullPolicy,
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
			Limits:   limits,
			Requests: container.Resources,
		},
	}
}

// RenderContainerSshd renders [corev1.Container] for sshd
func RenderContainerSshd(
	clusterType consts.ClusterType,
	container *values.Container,
	jailSubMounts []slurmv1.NodeVolumeJailSubMount,
	guaranteedQoS ...bool,
) corev1.Container {
	isGuaranteedQoS := false
	if len(guaranteedQoS) > 0 {
		isGuaranteedQoS = guaranteedQoS[0]
	}

	volumeMounts := []corev1.VolumeMount{
		RenderVolumeMountSlurmConfigs(),
		RenderVolumeMountJail(),
		RenderVolumeMountMungeSocket(),
		RenderVolumeMountSecurityLimits(),
		RenderVolumeMountSshdKeys(),
		RenderVolumeMountSshConfigs(),
		RenderVolumeMountSshRootKeys(),
	}
	volumeMounts = append(volumeMounts, RenderVolumeMountsForJailSubMounts(jailSubMounts)...)

	// Create a copy of the container's limits and add non-CPU resources from Requests
	var restartPolicy *corev1.ContainerRestartPolicy = nil

	limits := CopyNonCPUResources(container.Resources)
	if isGuaranteedQoS {
		// This configuration provides guaranteed QoS for the worker init container.
		limits = container.Resources
		policy := corev1.ContainerRestartPolicy("Always")
		restartPolicy = &policy
	}

	return corev1.Container{
		Name:          consts.ContainerNameSshd,
		Image:         container.Image,
		RestartPolicy: restartPolicy,
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
