package controller

import (
	corev1 "k8s.io/api/core/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/utils/stringutils"

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
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				},
			},
			AppArmorProfile: common.ParseAppArmorProfile(container.AppArmorProfile),
		},
		Resources: corev1.ResourceRequirements{
			Limits:   limits,
			Requests: container.Resources,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"scontrol",
						"ping",
					},
				},
			},
			InitialDelaySeconds: common.DefaultProbeInitialDelaySeconds,
			TimeoutSeconds:      common.DefaultProbeTimeoutSeconds,
			PeriodSeconds:       common.DefaultProbePeriodSeconds,
			SuccessThreshold:    common.DefaultProbeSuccessThreshold,
			FailureThreshold:    common.DefaultProbeFailureThreshold,
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}
}

// renderContainerSlurmctldSleep renders [corev1.Container] for slurmctld in sleep mode for DaemonSet
func renderContainerSlurmctldSleep(container *values.Container) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameSlurmctld,
		Image:           container.Image,
		ImagePullPolicy: container.ImagePullPolicy,
		Command:         []string{"sleep"},
		Args:            []string{"infinity"},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				},
			},
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}
}

// renderContainerAccountingWaiter renders accounting waiting init [corev1.Container]
func renderContainerAccountingWaiter(container *values.Container) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameWaitForAccounting,
		Image:           container.Image,
		ImagePullPolicy: container.ImagePullPolicy,
		Command: []string{
			"/bin/bash", "-c",
			// language=bash
			stringutils.Dedent(`
			set -eu
			
			ln -s /mnt/jail/etc/passwd /etc/passwd
			ln -s /mnt/jail/etc/group /etc/group
			ln -s /mnt/jail/etc/shadow /etc/shadow
			ln -s /mnt/jail/etc/gshadow /etc/gshadow
			chown -h 0:42 /etc/{shadow,gshadow}
			
			rm -rf /etc/slurm && ln -s /mnt/jail/etc/slurm /etc/slurm
			
			INTERVAL=5
			
			echo "Waiting until munge started"
			while [ ! -S "/run/munge/munge.socket.2" ]; do sleep ${INTERVAL}; done
			
			echo "Waiting for accounting via sacctmgr..."
			until sacctmgr -n --parsable2 list cluster >/dev/null 2>&1; do
			  echo "slurmdbd not ready yet, retrying..."
			  sleep ${INTERVAL}
			done
			echo "Accounting is ready."
			`),
		},
		VolumeMounts: []corev1.VolumeMount{
			common.RenderVolumeMountJail(),
			common.RenderVolumeMountMungeSocket(),
		},
	}
}
