package worker

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/check"
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
	jailSubMounts, customMounts []slurmv1.NodeVolumeMount,
	clusterName string,
	clusterType consts.ClusterType,
	cgroupVersion string,
	enableGDRCopy bool,
	slurmNodeExtra string,
) (corev1.Container, error) {
	volumeMounts := []corev1.VolumeMount{
		common.RenderVolumeMountSpool(consts.ComponentTypeWorker, consts.SlurmdName),
		common.RenderVolumeMountJail(),
		common.RenderVolumeMountMungeSocket(),
		common.RenderVolumeMountSecurityLimits(),
		common.RenderVolumeMountSshdKeys(),
		common.RenderVolumeMountSshdRootKeys(),
		common.RenderVolumeMountInMemory(),
		common.RenderVolumeMountTmpDisk(),
		renderVolumeMountSshdConfigs(),
		renderVolumeMountNvidia(),
		renderVolumeMountBoot(),
		renderVolumeMountNCCLTopology(),
		renderVolumeMountSharedMemory(),
		renderVolumeMountSysctl(),
		renderVolumeMountSupervisordConfigMap(),
	}
	volumeMounts = append(volumeMounts, common.RenderVolumeMounts(jailSubMounts, consts.VolumeMountPathJailUpper)...)
	volumeMounts = append(volumeMounts, common.RenderVolumeMounts(customMounts, "")...)

	resources := corev1.ResourceRequirements{
		Limits:   container.Resources,
		Requests: container.Resources,
	}

	err := check.CheckResourceRequests(resources)
	if err != nil {
		return corev1.Container{}, fmt.Errorf("checking resource requests: %w", err)
	}

	realMemory := renderRealMemorySlurmd(resources)

	return corev1.Container{
		Name:            consts.ContainerNameSlurmd,
		Image:           container.Image,
		ImagePullPolicy: container.ImagePullPolicy,
		Env: renderSlurmdEnv(
			clusterName,
			cgroupVersion,
			clusterType,
			realMemory,
			enableGDRCopy,
			slurmNodeExtra,
		),
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
						"show",
						"slurmd",
					},
				},
			},
			PeriodSeconds: 1,
		},
		// PreStop lifecycle hook to update the node state to down in case of worker deletion
		// Node will not be deleted from the slurm cluster if the job is still running
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/bash",
						"-c",
						"scontrol update nodename=$(hostname) state=down reason=preStop && scontrol delete nodename=$(hostname);",
					},
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
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeUnconfined,
			},
			ProcMount: ptr.To(corev1.UnmaskedProcMount),
		},
		Resources: resources,
	}, nil
}

func renderVolumeMountSupervisordConfigMap() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSupervisordConfigMap,
		MountPath: consts.VolumeMountPathSupervisordConfig,
		ReadOnly:  true,
	}
}

func renderSlurmdEnv(
	clusterName, cgroupVersion string,
	clusterType consts.ClusterType,
	realMemory int64,
	enableGDRCopy bool,
	slurmNodeExtra string,
) []corev1.EnvVar {
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
			Name: "INSTANCE_ID",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
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
		{
			Name:  "SLURM_NODE_EXTRA",
			Value: slurmNodeExtra,
		},
	}
	if cgroupVersion == consts.CGroupV2 {
		envVar = append(envVar, corev1.EnvVar{
			Name:  consts.CGroupV2Env,
			Value: "true",
		})
	}
	if enableGDRCopy {
		envVar = append(envVar, corev1.EnvVar{
			Name:  consts.NVIDIAGDRCopy,
			Value: "enabled",
		})
	}
	return envVar
}

func renderRealMemorySlurmd(resources corev1.ResourceRequirements) int64 {
	// Convert the memory quantity to bytes
	memoryInBytes := resources.Requests.Memory().Value()
	// Convert bytes to mebibytes (1 MiB = 1,048,576 bytes)
	memoryInMebibytes := memoryInBytes / 1_048_576 // 1 MiB = 1,048,576 bytes
	return memoryInMebibytes
}
