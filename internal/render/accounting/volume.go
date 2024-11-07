package accounting

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderVolumeMountSlurmConfigs renders [corev1.VolumeMount] defining the mounting path for Slurm config files
func RenderVolumeMountSlurmdbdConfigs() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSlurmdbdSecret,
		MountPath: consts.VolumeMountPathSlurmdbdSecret,
		ReadOnly:  true,
	}
}

func RenderVolumeSlurmdbdConfigs(clusterName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSlurmdbdSecret,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  naming.BuildSecretSlurmdbdConfigsName(clusterName),
				DefaultMode: ptr.To(int32(0600)),
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
