package accounting

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderVolumeProjectionSlurmdbdConfigs(clusterName string) *corev1.VolumeProjection {
	return &corev1.VolumeProjection{
		Secret: &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: naming.BuildSecretSlurmdbdConfigsName(clusterName),
			},
			Items: []corev1.KeyToPath{
				{
					Key:  consts.SlurmdbdConfFile,
					Path: consts.SlurmdbdConfFile,
					Mode: ptr.To(int32(0600)),
				},
			},
		},
	}
}

func RenderVolumeMountSlurmdbdSpool() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSpool,
		MountPath: consts.VolumeMountPathSpoolSlurmdbd,
		ReadOnly:  false,
	}
}

func RenderVolumeSlurmdbdSpool(accounting *values.SlurmAccounting) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSpool,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumDefault,
				SizeLimit: accounting.ContainerAccounting.Resources.Storage(),
			},
		},
	}
}

func RenderVolumeMountSlurmdbdSSLCACertificate() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSlurmdbdSSLCACertificate,
		MountPath: consts.VolumeMountPathSlurmdbdSSLCACertificate,
		ReadOnly:  true,
	}
}

func RenderVolumeSlurmdbdSSLCACertificate(secretName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSlurmdbdSSLCACertificate,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
				Items: []corev1.KeyToPath{
					{
						Key:  consts.SecretSlurmdbdSSLServerCACertificateFile,
						Path: consts.SecretSlurmdbdSSLServerCACertificateFile,
						Mode: ptr.To(consts.SecretSlurmdbdSSLServerCAFileMode),
					},
				},
				DefaultMode: ptr.To(common.DefaultFileMode),
			},
		},
	}
}

func RenderVolumeMountSlurmdbdSSLClientKey() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSlurmdbdSSLClientKey,
		MountPath: consts.VolumeMountPathSlurmdbdSSLClientKey,
		ReadOnly:  true,
	}
}

func RenderVolumeSlurmdbdSSLClientKey(secretName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSlurmdbdSSLClientKey,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
				Items: []corev1.KeyToPath{
					{
						Key:  consts.SecretSlurmdbdSSLClientKeyPrivateKeyFile,
						Path: consts.SecretSlurmdbdSSLClientKeyPrivateKeyFile,
						Mode: ptr.To(consts.SecretSlurmdbdSSLClientKeyFileMode),
					},
					{
						Key:  consts.SecretSlurmdbdSSLClientKeyCertificateFile,
						Path: consts.SecretSlurmdbdSSLClientKeyCertificateFile,
						Mode: ptr.To(consts.SecretSlurmdbdSSLClientKeyFileMode),
					},
				},
				DefaultMode: ptr.To(common.DefaultFileMode),
			},
		},
	}
}
