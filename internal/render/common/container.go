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

// RenderContainerSSSD renders [corev1.Container] for sssd
func RenderContainerSSSD(container *values.Container, opts ...RenderOption) corev1.Container {

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

	restartPolicy := corev1.ContainerRestartPolicy("Always")

	securityContext := &corev1.SecurityContext{
		AppArmorProfile: ParseAppArmorProfile(container.AppArmorProfile),
	}

	volumeMounts := []corev1.VolumeMount{
		RenderVolumeMountSSSDConf(),
		RenderVolumeMountSSSDSocket(),
	}
	if options.sssdLdapCAConfigMapName != "" {
		volumeMounts = append(volumeMounts, RenderVolumeMountSSSDLdapCA())
	}

	command, args := renderCommandAndArgsSSSD(container)

	return corev1.Container{
		Name:            consts.ContainerNameSSSD,
		Image:           container.Image,
		Command:         command,
		Args:            args,
		Env:             container.CustomEnv,
		RestartPolicy:   &restartPolicy,
		ImagePullPolicy: container.ImagePullPolicy,
		VolumeMounts:    volumeMounts,
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

func renderCommandAndArgsSSSD(container *values.Container) ([]string, []string) {
	command := container.Command
	args := container.Args

	if len(command) != 0 || len(args) != 0 {
		return command, args
	}

	return []string{"/bin/sh", "-c"}, []string{
		fmt.Sprintf(
			"mkdir -p %s \\\n&& chmod 700 %s \\\n&& exec /usr/sbin/sssd --interactive -d %d --logger=stderr",
			path.Join(consts.VolumeMountPathSSSDSocket, "private"),
			path.Join(consts.VolumeMountPathSSSDSocket, "private"),
			container.SSSDDebugLevel,
		),
	}
}

// RenderVolumeMountSSSDConf renders [corev1.VolumeMount] defining the mounting path for sssd config directory
func RenderVolumeMountSSSDConf() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSSSDConf,
		MountPath: consts.VolumeMountPathSSSDConf,
		ReadOnly:  true,
	}
}

// RenderVolumeMountSSSDSocket renders [corev1.VolumeMount] defining the mounting path for sssd socket
func RenderVolumeMountSSSDSocket() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSSSDSocket,
		MountPath: consts.VolumeMountPathSSSDSocket,
	}
}

// RenderVolumeMountSSSDLdapCA renders [corev1.VolumeMount] defining the mounting path for LDAP CA certificates.
func RenderVolumeMountSSSDLdapCA() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSSSDLdapCA,
		MountPath: consts.VolumeMountPathSSSDLdapCA,
		ReadOnly:  true,
	}
}
