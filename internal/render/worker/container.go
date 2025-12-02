package worker

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/utils/sliceutils"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderContainerWaitForController renders init [corev1.Container] that waits for controller readiness
func RenderContainerWaitForController(container *values.Container) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameWaitForController,
		Image:           container.Image,
		ImagePullPolicy: container.ImagePullPolicy,
		Command: []string{
			"/opt/bin/slurm/wait-for-controller.sh",
		},
		Env: []corev1.EnvVar{},
		VolumeMounts: []corev1.VolumeMount{
			common.RenderVolumeMountJail(),
			common.RenderVolumeMountMungeSocket(),
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
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
	workerFeatures []slurmv1.WorkerFeature,
	namespace string,
	useDefaultAppArmorProfile bool,
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

	realMemory := common.RenderRealMemorySlurmd(resources)

	appArmorProfile := container.AppArmorProfile
	if useDefaultAppArmorProfile {
		appArmorProfile = fmt.Sprintf("%s/%s", "localhost", naming.BuildAppArmorProfileName(clusterName, namespace))
	}

	return corev1.Container{
		Name:            consts.ContainerNameSlurmd,
		Image:           container.Image,
		ImagePullPolicy: container.ImagePullPolicy,
		Command:         container.Command,
		Args:            container.Args,
		Env: renderSlurmdEnv(
			clusterName,
			cgroupVersion,
			clusterType,
			realMemory,
			enableGDRCopy,
			slurmNodeExtra,
			workerFeatures,
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
			PeriodSeconds:    1,
			TimeoutSeconds:   common.DefaultProbeTimeoutSeconds,
			SuccessThreshold: common.DefaultProbeSuccessThreshold,
			FailureThreshold: common.DefaultProbeFailureThreshold,
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
			ProcMount:       ptr.To(corev1.UnmaskedProcMount),
			AppArmorProfile: common.ParseAppArmorProfile(appArmorProfile),
		},
		Resources:                resources,
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}, nil
}

// renderContainerNodeSetSlurmd renders [corev1.Container] for slurmd
func renderContainerNodeSetSlurmd(
	nodeSet *values.SlurmNodeSet,
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
		renderVolumeMountBoot(),
		renderVolumeMountSharedMemory(),
		renderVolumeMountSysctl(),
		renderVolumeMountSupervisordConfigMap(),
		renderVolumeMountSshdConfigs(),
	}
	if nodeSet.GPU.Enabled {
		volumeMounts = append(volumeMounts, renderVolumeMountNvidia())
	}

	// region Jail Sub-mounts
	volumeMounts = append(volumeMounts,
		common.RenderVolumeMounts(
			sliceutils.MapSlice(nodeSet.JailSubMounts,
				func(subMount slurmv1alpha1.NodeVolumeMount) slurmv1.NodeVolumeMount {
					return slurmv1.NodeVolumeMount{
						Name:      subMount.Name,
						MountPath: subMount.MountPath,
						SubPath:   subMount.SubPath,
						ReadOnly:  subMount.ReadOnly,
					}
				},
			),
			consts.VolumeMountPathJailUpper,
		)...,
	)
	// endregion Jail Sub-mounts

	// region Custom mounts
	volumeMounts = append(volumeMounts,
		common.RenderVolumeMounts(
			sliceutils.MapSlice(nodeSet.CustomVolumeMounts,
				func(mount slurmv1alpha1.NodeVolumeMount) slurmv1.NodeVolumeMount {
					return slurmv1.NodeVolumeMount{
						Name:      mount.Name,
						MountPath: mount.MountPath,
						SubPath:   mount.SubPath,
						ReadOnly:  mount.ReadOnly,
					}
				},
			),
			"",
		)...,
	)
	// endregion Custom mounts

	resources := corev1.ResourceRequirements{
		Limits:   nodeSet.ContainerSlurmd.Resources,
		Requests: nodeSet.ContainerSlurmd.Resources,
	}

	err := check.CheckResourceRequests(resources)
	if err != nil {
		return corev1.Container{}, fmt.Errorf("checking resource requests: %w", err)
	}

	appArmorProfile := nodeSet.ContainerSlurmd.AppArmorProfile
	if appArmorProfile == "" {
		if nodeSet.AppArmorProfileUseDefault {
			appArmorProfile = fmt.Sprintf("%s/%s", "localhost", naming.BuildAppArmorProfileName(nodeSet.ParentalCluster.Name, nodeSet.ParentalCluster.Namespace))
		} else {
			appArmorProfile = consts.AppArmorProfileUnconfined
		}
	}

	return corev1.Container{
		Name:            consts.ContainerNameSlurmd,
		Image:           nodeSet.ContainerSlurmd.Image,
		ImagePullPolicy: nodeSet.ContainerSlurmd.ImagePullPolicy,
		Command:         nodeSet.ContainerSlurmd.Command,
		Args:            nodeSet.ContainerSlurmd.Args,
		Env: renderNodeSetSlurmdEnv(
			nodeSet.CgroupVersion,
			utils.Ternary(nodeSet.GPU.Enabled, consts.ClusterTypeGPU, consts.ClusterTypeCPU),
			nodeSet.GPU.Nvidia.GDRCopyEnabled,
			nodeSet.NodeExtra,
		),
		Ports: []corev1.ContainerPort{{
			Name:          nodeSet.ContainerSlurmd.Name,
			ContainerPort: nodeSet.ContainerSlurmd.Port,
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
			PeriodSeconds:    1,
			TimeoutSeconds:   common.DefaultProbeTimeoutSeconds,
			SuccessThreshold: common.DefaultProbeSuccessThreshold,
			FailureThreshold: common.DefaultProbeFailureThreshold,
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
			ProcMount:       ptr.To(corev1.UnmaskedProcMount),
			AppArmorProfile: common.ParseAppArmorProfile(appArmorProfile),
		},
		Resources:                resources,
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
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
	workerFeatures []slurmv1.WorkerFeature,
) []corev1.EnvVar {
	envVar := []corev1.EnvVar{
		{
			Name: "K8S_POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: corev1.SchemeGroupVersion.Version,
					FieldPath:  "metadata.name",
				},
			},
		},
		{
			Name: "K8S_POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: corev1.SchemeGroupVersion.Version,
					FieldPath:  "metadata.namespace",
				},
			},
		},
		{
			Name: "INSTANCE_ID",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: corev1.SchemeGroupVersion.Version,
					FieldPath:  "spec.nodeName",
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
			Name:  consts.EnvCGroupV2,
			Value: "true",
		})
	}
	if enableGDRCopy {
		envVar = append(envVar, corev1.EnvVar{
			Name:  consts.EnvNvidiaGDRCopy,
			Value: "enabled",
		})
	}
	for _, feature := range workerFeatures {
		envVar = append(envVar, corev1.EnvVar{
			Name:  "SLURM_FEATURE_" + feature.Name,
			Value: feature.HostlistExpr,
		})
	}
	return envVar
}

func renderNodeSetSlurmdEnv(
	cgroupVersion string,
	clusterType consts.ClusterType,
	enableGDRCopy bool,
	slurmNodeExtra string,
) []corev1.EnvVar {
	envVar := []corev1.EnvVar{
		{
			Name: "INSTANCE_ID",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: corev1.SchemeGroupVersion.Version,
					FieldPath:  "spec.nodeName",
				},
			},
		},
		{
			Name:  "SLURM_CLUSTER_TYPE",
			Value: clusterType.String(),
		},
		{
			Name:  "SOPERATOR_NODE_SETS_ON",
			Value: "true",
		},
	}
	if len(slurmNodeExtra) > 0 {
		envVar = append(envVar, corev1.EnvVar{
			Name:  "SLURM_NODE_EXTRA",
			Value: slurmNodeExtra,
		})
	}
	if cgroupVersion == consts.CGroupV2 {
		envVar = append(envVar, corev1.EnvVar{
			Name:  consts.EnvCGroupV2,
			Value: "true",
		})
	}
	if enableGDRCopy {
		envVar = append(envVar, corev1.EnvVar{
			Name:  consts.EnvNvidiaGDRCopy,
			Value: "enabled",
		})
	}

	return envVar
}
