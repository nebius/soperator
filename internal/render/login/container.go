package login

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// renderContainerSshd renders [corev1.Container] for sshd
func renderContainerSshd(
	clusterWithGPU bool,
	container *values.Container,
	jailSubMounts, customMounts []slurmv1.NodeVolumeMount,
	containerSSSD *values.Container,
	userIsolation *slurmv1.LoginUserIsolation,
	dockerEnabled bool,
	dockerImageStorageMount *slurmv1.NodeVolumeMount,
	appArmorProfile string,
) corev1.Container {
	volumeMounts := []corev1.VolumeMount{
		common.RenderVolumeMountJail(),
		common.RenderVolumeMountMungeSocket(),
		common.RenderVolumeMountSecurityLimits(),
		common.RenderVolumeMountSshdKeys(),
		common.RenderVolumeMountSshdRootKeys(),
		common.RenderVolumeMountInMemory(),
		common.RenderVolumeMountTmpDisk(),
		renderVolumeMountSshdConfigs(),
	}
	if userIsolation != nil && ptr.Deref(userIsolation.Enabled, false) {
		volumeMounts = append(volumeMounts, renderVolumeMountUserIsolation())
	}
	if dockerEnabled {
		volumeMounts = append(
			volumeMounts,
			renderVolumeMountRuntime(),
			common.RenderVolumeMount(*dockerImageStorageMount, ""),
		)
	}
	if containerSSSD != nil {
		volumeMounts = append(volumeMounts,
			common.RenderVolumeMountSSSDSocket(),
			common.RenderVolumeMountSSSDConf(),
		)
	}
	volumeMounts = append(volumeMounts, common.RenderVolumeMounts(jailSubMounts, consts.VolumeMountPathJailUpper)...)
	volumeMounts = append(volumeMounts, common.RenderVolumeMounts(customMounts, "")...)
	env := []corev1.EnvVar{
		{
			Name:  "SLURM_CLUSTER_WITH_GPU",
			Value: strconv.FormatBool(clusterWithGPU),
		},
	}
	if dockerEnabled {
		env = append(env, corev1.EnvVar{
			Name:  consts.EnvDockerEnabled,
			Value: "true",
		})
	}
	env = append(env, container.CustomEnv...)

	// Create a copy of the container's limits and add non-CPU resources from Requests
	limits := common.CopyNonCPUResources(container.Resources)
	return corev1.Container{
		Name:            consts.ContainerNameSshd,
		Image:           container.Image,
		Command:         container.Command,
		Args:            container.Args,
		Env:             env,
		ImagePullPolicy: container.ImagePullPolicy,
		Ports: []corev1.ContainerPort{{
			Name:          container.Name,
			ContainerPort: container.Port,
			Protocol:      corev1.ProtocolTCP,
		}},
		VolumeMounts:   volumeMounts,
		LivenessProbe:  container.LivenessProbe,
		ReadinessProbe: container.ReadinessProbe,
		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				},
			},
			AppArmorProfile: common.ParseAppArmorProfile(appArmorProfile),
		},
		Resources: corev1.ResourceRequirements{
			Limits:   limits,
			Requests: container.Resources,
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}
}

func renderContainerDockerProxy(container *values.Container) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameDockerProxy,
		Image:           container.Image,
		ImagePullPolicy: container.ImagePullPolicy,
		Command:         []string{"/opt/bin/slurm/login-docker-proxy"},
		VolumeMounts: []corev1.VolumeMount{
			renderVolumeMountRuntime(),
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}
}

func renderVolumeMountRuntime() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameRuntime,
		MountPath: consts.VolumeMountPathRuntime,
	}
}
