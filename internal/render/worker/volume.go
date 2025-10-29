package worker

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// region Volumes & claims

func renderVolumesAndClaimTemplateSpecs(
	clusterName string,
	secrets *slurmv1.Secrets,
	volumeSources []slurmv1.VolumeSource,
	worker *values.SlurmWorker,
) (volumes []corev1.Volume, pvcTemplateSpecs []values.PVCTemplateSpec, err error) {

	volumes = []corev1.Volume{
		common.RenderVolumeMungeKey(clusterName),
		common.RenderVolumeMungeSocket(),
		common.RenderVolumeSecurityLimits(clusterName, consts.ComponentTypeWorker),
		common.RenderVolumeSshdKeys(secrets.SshdKeysName),
		common.RenderVolumeSshdRootKeys(clusterName),
		common.RenderVolumeInMemory(worker.ContainerSlurmd.Resources.Memory()),
		common.RenderVolumeTmpDisk(),
		renderVolumeSshdConfigs(worker.SSHDConfigMapName),
		renderVolumeNvidia(),
		renderVolumeBoot(),
		renderVolumeSharedMemory(worker.SharedMemorySize),
		renderVolumeSysctl(clusterName),
		renderSupervisordConfigMap(worker.SupervisordConfigMapName),
	}

	// Spool and Jail could be specified by template spec or by volume source name
	{
		if v, s, err := common.AddVolumeOrSpec(
			worker.VolumeSpool.VolumeSourceName,
			func(sourceName string) corev1.Volume {
				return common.RenderVolumeSpoolFromSource(
					consts.ComponentTypeWorker,
					volumeSources,
					sourceName,
				)
			},
			worker.VolumeSpool.VolumeClaimTemplateSpec,
			common.RenderVolumeNameSpool(consts.ComponentTypeWorker),
		); err != nil {
			return nil, nil, err
		} else {
			volumes = append(volumes, v...)
			pvcTemplateSpecs = append(pvcTemplateSpecs, s...)
		}

		if v, s, err := common.AddVolumeOrSpec(
			worker.VolumeJail.VolumeSourceName,
			func(sourceName string) corev1.Volume {
				return common.RenderVolumeJailFromSource(volumeSources, sourceName)
			},
			worker.VolumeJail.VolumeClaimTemplateSpec,
			consts.VolumeNameJail,
		); err != nil {
			return nil, nil, err
		} else {
			volumes = append(volumes, v...)
			pvcTemplateSpecs = append(pvcTemplateSpecs, s...)
		}
	}

	// Jail sub-mounts
	for _, subMount := range worker.JailSubMounts {
		if v, s, err := common.AddVolumeOrSpec(
			subMount.VolumeSourceName,
			func(sourceName string) corev1.Volume {
				return common.RenderVolumeFromSource(volumeSources, *subMount.VolumeSourceName, subMount.Name)
			},
			subMount.VolumeClaimTemplateSpec,
			subMount.Name,
		); err != nil {
			return nil, nil, err
		} else {
			volumes = append(volumes, v...)
			pvcTemplateSpecs = append(pvcTemplateSpecs, s...)
		}
	}

	// Custom mounts
	for _, customMount := range worker.CustomVolumeMounts {
		if v, s, err := common.AddVolumeOrSpec(
			customMount.VolumeSourceName,
			func(sourceName string) corev1.Volume {
				return common.RenderVolumeFromSource(volumeSources, *customMount.VolumeSourceName, customMount.Name)
			},
			customMount.VolumeClaimTemplateSpec,
			customMount.Name,
		); err != nil {
			return nil, nil, err
		} else {
			volumes = append(volumes, v...)
			pvcTemplateSpecs = append(pvcTemplateSpecs, s...)
		}
	}

	return volumes, pvcTemplateSpecs, nil
}

func renderVolumesAndClaimTemplateSpecsForNodeSet(
	nodeSet *values.SlurmNodeSet,
	secrets *slurmv1.Secrets,
) (volumes []corev1.Volume, pvcTemplateSpecs []values.PVCTemplateSpec, err error) {
	volumes = []corev1.Volume{
		common.RenderVolumeMungeKey(nodeSet.ParentalCluster.Name),
		common.RenderVolumeMungeSocket(),
		common.RenderVolumeSecurityLimitsForNodeSet(nodeSet.ParentalCluster.Name, nodeSet.Name),
		common.RenderVolumeSshdKeys(secrets.SshdKeysName),
		common.RenderVolumeSshdRootKeys(nodeSet.ParentalCluster.Name),
		common.RenderVolumeInMemory(nodeSet.ContainerSlurmd.Resources.Memory()),
		common.RenderVolumeTmpDisk(),
		renderVolumeBoot(),
		renderVolumeSharedMemory(nodeSet.SharedMemorySize),
		renderVolumeSysctl(nodeSet.ParentalCluster.Name),
		renderSupervisordConfigMap(nodeSet.SupervisorDConfigMapName),
		renderVolumeSshdConfigs(nodeSet.SSHDConfigMapName),
	}
	if nodeSet.GPU.Enabled {
		volumes = append(volumes, renderVolumeNvidia())
	}

	// region Worker Spool
	volumes = append(volumes,
		corev1.Volume{
			Name:         common.RenderVolumeNameSpool(consts.ComponentTypeWorker),
			VolumeSource: nodeSet.VolumeSpool,
		},
	)
	// endregion Worker Spool

	// region Jail
	volumes = append(volumes,
		corev1.Volume{
			Name:         consts.VolumeNameJail,
			VolumeSource: nodeSet.VolumeJail,
		},
	)
	// endregion Jail

	// region Jail sub-mounts
	for _, subMount := range nodeSet.JailSubMounts {
		if v, s, err := common.AddVolumeOrSpecVanilla(subMount.Name, subMount.VolumeSource, subMount.VolumeClaimTemplateSpec); err != nil {
			return nil, nil, err
		} else {
			volumes = append(volumes, v...)
			pvcTemplateSpecs = append(pvcTemplateSpecs, s...)
		}
	}
	// endregion Jail sub-mounts

	// region Custom mounts
	for _, customMount := range nodeSet.CustomVolumeMounts {
		if v, s, err := common.AddVolumeOrSpecVanilla(customMount.Name, customMount.VolumeSource, customMount.VolumeClaimTemplateSpec); err != nil {
			return nil, nil, err
		} else {
			volumes = append(volumes, v...)
			pvcTemplateSpecs = append(pvcTemplateSpecs, s...)
		}
	}
	// endregion Custom mounts

	return volumes, pvcTemplateSpecs, nil
}

func renderSupervisordConfigMap(name string) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSupervisordConfigMap,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: name,
				},
				DefaultMode: ptr.To(common.DefaultFileMode),
			},
		},
	}
}

// endregion Volumes & claims

// region Nvidia

// renderVolumeNvidia renders [corev1.Volume] containing nvidia contents
func renderVolumeNvidia() corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameNvidia,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: consts.VolumeMountPathNvidia,
				Type: ptr.To(corev1.HostPathType("")),
			},
		},
	}
}

// renderVolumeMountNvidia renders [corev1.VolumeMount] defining the mounting path for nvidia
func renderVolumeMountNvidia() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:             consts.VolumeNameNvidia,
		MountPath:        consts.VolumeMountPathNvidia,
		MountPropagation: ptr.To(corev1.MountPropagationHostToContainer),
	}
}

// endregion Nvidia

// region Boot

// renderVolumeBoot renders [corev1.Volume] containing boot contents
func renderVolumeBoot() corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameBoot,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: consts.VolumeMountPathBoot,
				Type: ptr.To(corev1.HostPathType("")),
			},
		},
	}
}

// renderVolumeMountBoot renders [corev1.VolumeMount] defining the mounting path for boot
func renderVolumeMountBoot() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:             consts.VolumeNameBoot,
		MountPath:        consts.VolumeMountPathBoot,
		MountPropagation: ptr.To(corev1.MountPropagationHostToContainer),
	}
}

// endregion Boot

// region Shared memory

// renderVolumeSharedMemory renders [corev1.Volume] containing shared memory contents
func renderVolumeSharedMemory(sizeSharedMemory *resource.Quantity) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSharedMemory,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: sizeSharedMemory,
			},
		},
	}
}

// renderVolumeMountSharedMemory renders [corev1.VolumeMount] defining the mounting path for shared memory
func renderVolumeMountSharedMemory() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSharedMemory,
		MountPath: consts.VolumeMountPathSharedMemory,
	}
}

// endregion Shared memory

// region Sysctl

// renderVolumeSysctl renders [corev1.Volume] containing sysctl config contents
func renderVolumeSysctl(clusterName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSysctl,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: naming.BuildConfigMapSysctlName(clusterName),
				},
				DefaultMode: ptr.To(common.DefaultFileMode),
			},
		},
	}
}

// renderVolumeMountSysctl renders [corev1.VolumeMount] defining the mounting path for sysctl config
func renderVolumeMountSysctl() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSysctl,
		MountPath: consts.VolumeMountPathSysctl,
		SubPath:   consts.VolumeMountSubPathSysctl,
	}
}

// endregion Sysctl

// region configs

// RenderVolumeSshdConfigs renders [corev1.Volume] containing SSHD configs contents
func renderVolumeSshdConfigs(sshdConfigMapName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSSHDConfigsWorker,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: sshdConfigMapName,
				},
				DefaultMode: ptr.To(common.DefaultFileMode),
			},
		},
	}
}

// RenderVolumeMountSshdConfigs renders [corev1.VolumeMount] defining the mounting path for SSHD configs
func renderVolumeMountSshdConfigs() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSSHDConfigsWorker,
		MountPath: consts.VolumeMountPathSSHConfigs,
		ReadOnly:  true,
	}
}

// endregion configs
