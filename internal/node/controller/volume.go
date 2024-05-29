package controller

import (
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
)

// renderVolumeSlurmKey renders [corev1.Volume] containing Slurm key file
func renderVolumeSlurmKey() corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeSlurmKeyName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: consts.SecretSlurmKeyName,
				Items: []corev1.KeyToPath{
					{
						Key:  consts.SecretSlurmKeySlurmKeyKey,
						Path: consts.SecretSlurmKeySlurmKeyPath,
						Mode: &consts.SecretSlurmKeySlurmKeyMode,
					},
				},
			},
		},
	}
}

// renderVolumeMountSlurmKey renders [corev1.VolumeMount] defining the mounting path for Slurm key file
func renderVolumeMountSlurmKey() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeSlurmKeyName,
		MountPath: "/root/slurm-k8s-conf/key",
		ReadOnly:  true,
	}
}

// renderVolumeSlurmConfigs renders [corev1.Volume] containing Slurm config files
func renderVolumeSlurmConfigs() corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeSlurmConfigsName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: consts.ConfigMapSlurmConfigsName},
			},
		},
	}
}

// renderVolumeMountSlurmConfigs renders [corev1.VolumeMount] defining the mounting path for Slurm config files
func renderVolumeMountSlurmConfigs() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeSlurmConfigsName,
		MountPath: "/root/slurm-k8s-conf/configs",
		ReadOnly:  true,
	}
}

// renderVolumeSpool renders [corev1.Volume] containing spool contents
func renderVolumeSpool() corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeSlurmConfigsName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: consts.PVCControllerSpoolName,
				ReadOnly:  false,
			},
		},
	}
}

// renderVolumeMountSpool renders [corev1.VolumeMount] defining the mounting path for spool contents
func renderVolumeMountSpool() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeSlurmSpoolName,
		MountPath: "/var/spool",
	}
}
