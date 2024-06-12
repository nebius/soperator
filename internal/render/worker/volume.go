package worker

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
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
		common.RenderVolumeSlurmConfigs(clusterName),
		common.RenderVolumeMungeKey(secrets.MungeKey.Name, secrets.MungeKey.Key),
		common.RenderVolumeMungeSocket(),
		renderVolumeNvidia(),
		renderVolumeBoot(),
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
		volumes = append(
			volumes,
			common.RenderVolumeFromSource(volumeSources, subMount.VolumeSourceName, subMount.Name),
		)
	}

	return volumes, pvcTemplateSpecs, nil
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
