package worker

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"nebius.ai/slurm-operator/internal/naming"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// renderContainerToolkitValidation renders init [corev1.Container] for toolkit validation
func renderContainerToolkitValidation(container *values.Container) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameToolkitValidation,
		Image:           container.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{
			"sh",
		},
		Args: []string{
			"-c",
			strings.Join(
				[]string{
					fmt.Sprintf("until [ -f %s/validations/toolkit-ready ]; do", consts.VolumeMountPathNvidia),
					"echo 'waiting for nvidia container stack to be setup';",
					"sleep 5;",
					"done",
				},
				" ",
			),
		},
		VolumeMounts: []corev1.VolumeMount{
			renderVolumeMountNvidia(),
		},
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		TerminationMessagePath:   "/dev/termination-log",
	}
}

func renderContainerCgroupMaker(container *values.Container) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameCgroupMaker,
		Image:           container.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: *container.Resources.Memory(),
			},
			Requests: container.Resources,
		},
		Command: []string{
			"sh",
		},
		Args: []string{
			"-c",
			strings.Join(
				[]string{
					`export CGROUP_PATH=$(cat /proc/self/cgroup | awk -F'/' '{print "/"$2"/"$3"/"$4}');`,
					`if [ -n "${CGROUP_PATH}" ]; then`,
					`echo "cgroup v2 detected, creating cgroup for ${CGROUP_PATH}";`,
					`mkdir -p /sys/fs/cgroup/${CGROUP_PATH}/system.slice;`,
					`fi`,
				},
				" ",
			),
		},

		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(true),
			Capabilities: &corev1.Capabilities{

				Drop: []corev1.Capability{"ALL"},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeUnconfined,
			},
			ProcMount: ptr.To(corev1.UnmaskedProcMount),
		},
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		TerminationMessagePath:   "/dev/termination-log",
	}
}

// renderContainerSlurmd renders [corev1.Container] for slurmd
func renderContainerSlurmd(
	container *values.Container,
	jailSubMounts []slurmv1.NodeVolumeJailSubMount,
	clusterName string,
) corev1.Container {
	volumeMounts := []corev1.VolumeMount{
		common.RenderVolumeMountSlurmConfigs(),
		common.RenderVolumeMountSpool(consts.ComponentTypeWorker, consts.SlurmdName),
		common.RenderVolumeMountJail(),
		common.RenderVolumeMountMungeSocket(),
		renderVolumeMountNvidia(),
		renderVolumeMountBoot(),
		renderVolumeMountNCCLTopology(),
		renderVolumeMountSharedMemory(),
		renderVolumeMountSysctl(),
	}
	volumeMounts = append(volumeMounts, common.RenderVolumeMountsForJailSubMounts(jailSubMounts)...)

	return corev1.Container{
		Name:            consts.ContainerNameSlurmd,
		Image:           container.Image,
		ImagePullPolicy: corev1.PullAlways, // TODO use digest and set to corev1.PullIfNotPresent
		Env: []corev1.EnvVar{
			{
				Name: "K8S_POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			}, {
				Name: "K8S_POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			}, {
				Name:  "K8S_SERVICE_NAME",
				Value: naming.BuildServiceName(consts.ComponentTypeWorker, clusterName),
			},
		},
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
						"/bin/sh",
						"-c",
						"/usr/bin/sinfo > /dev/null && exit 0 || exit 1",
					},
				},
			},
			PeriodSeconds: 30,
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				},
			},
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeUnconfined,
			},
			ProcMount: ptr.To(corev1.UnmaskedProcMount),
		},
		Resources: corev1.ResourceRequirements{
			Limits:   container.Resources,
			Requests: container.Resources,
		},
	}
}
