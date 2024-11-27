package worker

import (
	"fmt"
	"strconv"
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
		ImagePullPolicy: container.ImagePullPolicy,
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

// renderContainerSlurmd renders [corev1.Container] for slurmd
func renderContainerSlurmd(
	container *values.Container,
	jailSubMounts []slurmv1.NodeVolumeJailSubMount,
	clusterName string,
	clusterType consts.ClusterType,
	cgroupVersion string,
) corev1.Container {
	volumeMounts := []corev1.VolumeMount{
		common.RenderVolumeMountSlurmConfigs(),
		common.RenderVolumeMountSpool(consts.ComponentTypeWorker, consts.SlurmdName),
		common.RenderVolumeMountJail(),
		common.RenderVolumeMountMungeSocket(),
		common.RenderVolumeMountSecurityLimits(),
		renderVolumeMountNvidia(),
		renderVolumeMountBoot(),
		renderVolumeMountNCCLTopology(),
		renderVolumeMountSharedMemory(),
		renderVolumeMountSysctl(),
	}
	volumeMounts = append(volumeMounts, common.RenderVolumeMountsForJailSubMounts(jailSubMounts)...)

	resources := corev1.ResourceRequirements{
		Limits:   container.Resources,
		Requests: container.Resources,
	}

	readlMemory := renderRealMemorySlurmd(resources)

	return corev1.Container{
		Name:            consts.ContainerNameSlurmd,
		Image:           container.Image,
		ImagePullPolicy: container.ImagePullPolicy,
		Env:             renderSlurmdEnv(clusterName, cgroupVersion, clusterType, readlMemory),
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
		Resources: resources,
	}
}

func renderSlurmdEnv(clusterName, cgroupVersion string, clusterType consts.ClusterType, realMemory int64) []corev1.EnvVar {
	envVar := []corev1.EnvVar{
		{
			Name: "K8S_POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "K8S_POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
		{
			Name:  "K8S_SERVICE_NAME",
			Value: naming.BuildServiceName(consts.ComponentTypeWorker, clusterName),
		},
		{
			Name:  "SLURM_CLUSTER_TYPE",
			Value: clusterType.String(),
		},
		{
			Name:  "SLURM_REAL_MEMORY",
			Value: strconv.FormatInt(realMemory, 10),
		},
	}
	if cgroupVersion == consts.CGroupV2 {
		envVar = append(envVar, corev1.EnvVar{
			Name:  consts.CGroupV2Env,
			Value: "true",
		})
	}
	return envVar
}

// Convert to bytes and then to megabytes for real memory value
func renderRealMemorySlurmd(resources corev1.ResourceRequirements) int64 {
	// Convert the memory quantity to bytes
	memoryInBytes := resources.Requests.Memory().Value()
	// Convert bytes to megabytes (DecimalSI uses 1000 for conversions)
	memoryInMegabytes := memoryInBytes / 1_000_000 // 1 MB = 1,000,000 bytes
	return memoryInMegabytes
}
