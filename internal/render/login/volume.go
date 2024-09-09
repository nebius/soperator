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
		common.RenderVolumeMungeKey(clusterName),
		common.RenderVolumeMungeSocket(),
		common.RenderVolumeSecurityLimits(clusterName, consts.ComponentTypeLogin),
		renderVolumeSshdKeys(secrets.SshdKeysName),
		renderVolumeSshConfigs(clusterName),
		renderVolumeSshRootKeys(clusterName),
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
func renderVolumeSshRootKeys(clusterName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSSHRootKeys,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: naming.BuildConfigMapSshRootPublicKeysName(clusterName),
				},
				DefaultMode: ptr.To(int32(384)),
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

// region sshd keys

// renderVolumeSshdKeys renders [corev1.Volume] containing SSHD key pairs
func renderVolumeSshdKeys(sshdKeysSecretName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSSHDKeys,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: sshdKeysSecretName,
				Items: []corev1.KeyToPath{
					{
						Key:  consts.SecretSshdECDSAKeyName,
						Path: consts.SecretSshdECDSAKeyName,
						Mode: ptr.To(consts.SecretSshdKeysPrivateFileMode),
					},
					{
						Key:  consts.SecretSshdECDSAPubKeyName,
						Path: consts.SecretSshdECDSAPubKeyName,
						Mode: ptr.To(consts.SecretSshdKeysPublicFileMode),
					},
					{
						Key:  consts.SecretSshdECDSA25519KeyName,
						Path: consts.SecretSshdECDSA25519KeyName,
						Mode: ptr.To(consts.SecretSshdKeysPrivateFileMode),
					},
					{
						Key:  consts.SecretSshdECDSA25519PubKeyName,
						Path: consts.SecretSshdECDSA25519PubKeyName,
						Mode: ptr.To(consts.SecretSshdKeysPublicFileMode),
					},
					{
						Key:  consts.SecretSshdRSAKeyName,
						Path: consts.SecretSshdRSAKeyName,
						Mode: ptr.To(consts.SecretSshdKeysPrivateFileMode),
					},
					{
						Key:  consts.SecretSshdRSAPubKeyName,
						Path: consts.SecretSshdRSAPubKeyName,
						Mode: ptr.To(consts.SecretSshdKeysPublicFileMode),
					},
				},
			},
		},
	}
}

// renderVolumeMountSshdKeys renders [corev1.VolumeMount] defining the mounting path for SSHD key pairs
func renderVolumeMountSshdKeys() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSSHDKeys,
		MountPath: consts.VolumeMountPathSSHDKeys,
		ReadOnly:  true,
	}
}

// endregion sshd keys

// endregion SSH
