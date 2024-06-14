package login

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func renderVolumesAndClaimTemplateSpecs(
	clusterName string,
	secrets *slurmv1.Secrets,
	volumeSources []slurmv1.VolumeSource,
	login *values.SlurmLogin,
) (volumes []corev1.Volume, pvcTemplateSpecs []values.PVCTemplateSpec, err error) {
	volumes = []corev1.Volume{
		common.RenderVolumeSlurmConfigs(clusterName),
		common.RenderVolumeMungeKey(secrets.MungeKey.Name, secrets.MungeKey.Key),
		common.RenderVolumeMungeSocket(),
		renderVolumeSshConfigs(clusterName),
		renderVolumeSshRootKeys(*secrets.SSHRootPublicKeys),
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
		volumes = append(
			volumes,
			common.RenderVolumeFromSource(volumeSources, subMount.VolumeSourceName, subMount.Name),
		)
	}

	return volumes, pvcTemplateSpecs, nil
}

// region SSH

// region configs

// renderVolumeSshConfigs renders [corev1.Volume] containing SSH configs contents
func renderVolumeSshConfigs(clusterName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSSHConfigs,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: naming.BuildConfigMapSSHConfigsName(clusterName),
				},
			},
		},
	}
}

// renderVolumeMountSshConfigs renders [corev1.VolumeMount] defining the mounting path for SSH configs
func renderVolumeMountSshConfigs() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSSHConfigs,
		MountPath: consts.VolumeMountPathSSHConfigs,
		ReadOnly:  true,
	}
}

// endregion configs

// region root keys

// renderVolumeSshRootKeys renders [corev1.Volume] containing SSH root keys contents
func renderVolumeSshRootKeys(secret slurmv1.SecretKey) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSSHRootKeys,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secret.Name,
				Items: []corev1.KeyToPath{{
					Key:  secret.Key,
					Path: consts.VolumeMountSubPathSSHRootKeys,
				}},
				DefaultMode: ptr.To(int32(0600)),
			},
		},
	}
}

// renderVolumeMountSshRootKeys renders [corev1.VolumeMount] defining the mounting path for SSH root keys
func renderVolumeMountSshRootKeys() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSSHRootKeys,
		MountPath: consts.VolumeMountPathSSHRootKeys,
		SubPath:   consts.VolumeMountSubPathSSHRootKeys,
		ReadOnly:  true,
	}
}

// endregion root keys

// endregion SSH
