package login

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

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
	// TODO: should we remove slurmTopologyConfigMapRefName?
	// It was added here: https://github.com/nebius/soperator/pull/512/files#diff-7b22c3a71b5e99d01467ef50b9d060d03cf76d9cede340519df7d4ed99e75fd2
	// and then it was removed here: https://github.com/nebius/soperator/pull/543/files#diff-7b22c3a71b5e99d01467ef50b9d060d03cf76d9cede340519df7d4ed99e75fd2
	_ = slurmTopologyConfigMapRefName

	volumes = []corev1.Volume{
		common.RenderVolumeMungeKey(clusterName),
		common.RenderVolumeMungeSocket(),
		common.RenderVolumeSecurityLimits(clusterName, consts.ComponentTypeLogin),
		common.RenderVolumeSshdKeys(secrets.SshdKeysName),
		common.RenderVolumeSshdRootKeys(clusterName),
		common.RenderVolumeInMemory(),
		common.RenderVolumeTmpDisk(),
		renderVolumeSshdConfigs(login.SSHDConfigMapName),
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

// region configs

// RenderVolumeSshdConfigs renders [corev1.Volume] containing SSHD configs contents
func renderVolumeSshdConfigs(sshdConfigMapName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSSHDConfigsLogin,
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
		Name:      consts.VolumeNameSSHDConfigsLogin,
		MountPath: consts.VolumeMountPathSSHConfigs,
		ReadOnly:  true,
	}
}

// endregion configs
