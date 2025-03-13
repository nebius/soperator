package login

import (
	corev1 "k8s.io/api/core/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func renderVolumesAndClaimTemplateSpecs(
	clusterName string,
	secrets *slurmv1.Secrets,
	volumeSources []slurmv1.VolumeSource,
	login *values.SlurmLogin,
	slurmTopologyConfigMapRefName string,
) (volumes []corev1.Volume, pvcTemplateSpecs []values.PVCTemplateSpec, err error) {
	volumes = []corev1.Volume{
		common.RenderVolumeProjectedSlurmConfigs(
			clusterName,
			common.RenderVolumeProjectionSlurmTopologyConfig(slurmTopologyConfigMapRefName),
		),
		common.RenderVolumeMungeKey(clusterName),
		common.RenderVolumeMungeSocket(),
		common.RenderVolumeSecurityLimits(clusterName, consts.ComponentTypeLogin),
		common.RenderVolumeSshdKeys(secrets.SshdKeysName),
		common.RenderVolumeSshdConfigs(login.SSHDConfigMapName),
		common.RenderVolumeSshdRootKeys(clusterName),
		common.RenderVolumeInMemory(),
		common.RenderVolumeTmpDisk(),
	}

	// Jail could be specified by template spec or by volume source name
	if v, s, err := common.AddVolumeOrSpec(
		login.VolumeJail.VolumeSourceName,
		func(sourceName string) corev1.Volume {
			return common.RenderVolumeJailFromSource(volumeSources, sourceName)
		},
		login.VolumeJail.VolumeClaimTemplateSpec,
		consts.VolumeNameJail,
	); err != nil {
		return nil, nil, err
	} else {
		volumes = append(volumes, v...)
		pvcTemplateSpecs = append(pvcTemplateSpecs, s...)
	}

	// Jail sub-mounts
	for _, subMount := range login.JailSubMounts {
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
	for _, customMount := range login.CustomVolumeMounts {
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
