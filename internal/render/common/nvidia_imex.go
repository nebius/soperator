package common

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/consts"
)

func RenderVolumesNvidiaIMEX() []corev1.Volume {
	return []corev1.Volume{
		renderVolumeNvidiaIMEXCLI(consts.VolumeNameNvidiaIMEXCLI),
		renderVolumeNvidiaIMEXCLI(consts.VolumeNameNvidiaIMEXCLIJail),
		renderVolumeNvidiaIMEXConfig(consts.VolumeNameNvidiaIMEXConfig),
		renderVolumeNvidiaIMEXConfig(consts.VolumeNameNvidiaIMEXConfigJail),
	}
}

func RenderVolumeMountsNvidiaIMEX() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      consts.VolumeNameNvidiaIMEXCLI,
			MountPath: consts.NvidiaIMEXCLIMountPath,
			ReadOnly:  true,
		},
		{
			Name:      consts.VolumeNameNvidiaIMEXCLIJail,
			MountPath: consts.NvidiaIMEXCLIJailPath,
			ReadOnly:  true,
		},
		{
			Name:      consts.VolumeNameNvidiaIMEXConfig,
			MountPath: consts.NvidiaIMEXConfigMountPath,
			ReadOnly:  true,
		},
		{
			Name:      consts.VolumeNameNvidiaIMEXConfigJail,
			MountPath: consts.NvidiaIMEXConfigJailPath,
			ReadOnly:  true,
		},
	}
}

func renderVolumeNvidiaIMEXCLI(name string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: consts.NvidiaIMEXCLIHostPath,
				Type: ptr.To(corev1.HostPathFileOrCreate),
			},
		},
	}
}

func renderVolumeNvidiaIMEXConfig(name string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: consts.NvidiaIMEXConfigHostPath,
				Type: ptr.To(corev1.HostPathDirectoryOrCreate),
			},
		},
	}
}
