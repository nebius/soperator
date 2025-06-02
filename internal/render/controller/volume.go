package controller

import (
	corev1 "k8s.io/api/core/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func renderVolumesAndClaimTemplateSpecs(
	clusterName string,
	volumeSources []slurmv1.VolumeSource,
	controller *values.SlurmController,
	slurmTopologyConfigMapRefName string,
) (volumes []corev1.Volume, pvcTemplateSpecs []values.PVCTemplateSpec, err error) {
	// TODO: should we remove slurmTopologyConfigMapRefName?
	// It was added here:https://github.com/nebius/soperator/pull/512/files#diff-2791182e727336ce7ed0d512bacffce1971ad4eed34bdc27d6fb8be612e1b147
	// and then it was removed here: https://github.com/nebius/soperator/pull/543/files#diff-2791182e727336ce7ed0d512bacffce1971ad4eed34bdc27d6fb8be612e1b147
	_ = slurmTopologyConfigMapRefName
	volumes = []corev1.Volume{
		common.RenderVolumeMungeKey(clusterName),
		common.RenderVolumeMungeSocket(),
		common.RenderVolumeSecurityLimits(clusterName, consts.ComponentTypeController),
		common.RenderVolumeRESTJWTKey(clusterName),
	}

	// Spool, Jail and CustomVolumes could be specified by template spec or by volume source name
	{
		if v, s, err := common.AddVolumeOrSpec(
			controller.VolumeSpool.VolumeSourceName,
			func(sourceName string) corev1.Volume {
				return common.RenderVolumeSpoolFromSource(
					consts.ComponentTypeController,
					volumeSources,
					sourceName,
				)
			},
			controller.VolumeSpool.VolumeClaimTemplateSpec,
			common.RenderVolumeNameSpool(consts.ComponentTypeController),
		); err != nil {
			return nil, nil, err
		} else {
			volumes = append(volumes, v...)
			pvcTemplateSpecs = append(pvcTemplateSpecs, s...)
		}

		if v, s, err := common.AddVolumeOrSpec(
			controller.VolumeJail.VolumeSourceName,
			func(sourceName string) corev1.Volume {
				return common.RenderVolumeJailFromSource(volumeSources, sourceName)
			},
			controller.VolumeJail.VolumeClaimTemplateSpec,
			consts.VolumeNameJail,
		); err != nil {
			return nil, nil, err
		} else {
			volumes = append(volumes, v...)
			pvcTemplateSpecs = append(pvcTemplateSpecs, s...)
		}

		for _, customMount := range controller.CustomVolumeMounts {
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
	}

	return volumes, pvcTemplateSpecs, nil
}
