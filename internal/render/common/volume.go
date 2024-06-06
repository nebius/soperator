package common

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// region PVC template

func RenderVolumeClaimTemplates(componentType consts.ComponentType, cluster *values.SlurmCluster, pvcTemplateSpecs []values.PVCTemplateSpec) []corev1.PersistentVolumeClaim {
	var res []corev1.PersistentVolumeClaim
	for _, template := range pvcTemplateSpecs {
		if template.Spec == nil {
			continue
		}
		res = append(res, RenderVolumeClaimTemplate(componentType, cluster, template.Name, *template.Spec))
	}
	return res
}

func RenderVolumeClaimTemplate(componentType consts.ComponentType, cluster *values.SlurmCluster, pvcName string, pvcClaimSpec corev1.PersistentVolumeClaimSpec) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: cluster.Namespace,
			Labels:    RenderLabels(componentType, cluster.Name),
		},
		Spec: pvcClaimSpec,
	}
}

// endregion PVC template

// region Slurm key

// RenderVolumeSlurmKey renders [corev1.Volume] containing Slurm key file
func RenderVolumeSlurmKey(cluster *values.SlurmCluster) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeSlurmKeyName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: cluster.Secrets.SlurmKey.Name,
				Items: []corev1.KeyToPath{
					{
						Key:  cluster.Secrets.SlurmKey.Key,
						Path: consts.SecretSlurmKeyFileName,
						Mode: &consts.SecretSlurmKeyFileMode,
					},
				},
			},
		},
	}
}

// RenderVolumeMountSlurmKey renders [corev1.VolumeMount] defining the mounting path for Slurm key file
func RenderVolumeMountSlurmKey() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeSlurmKeyName,
		MountPath: consts.VolumeSlurmKeyMountPath,
		ReadOnly:  true,
	}
}

// endregion Slurm key

// region Slurm configs

// RenderVolumeSlurmConfigs renders [corev1.Volume] containing Slurm config files
func RenderVolumeSlurmConfigs(cluster *values.SlurmCluster) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeSlurmConfigsName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: cluster.ConfigMapSlurmConfigs.Name},
			},
		},
	}
}

// RenderVolumeMountSlurmConfigs renders [corev1.VolumeMount] defining the mounting path for Slurm config files
func RenderVolumeMountSlurmConfigs() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeSlurmConfigsName,
		MountPath: consts.VolumeSlurmConfigsMountPath,
		ReadOnly:  true,
	}
}

// endregion Slurm configs

// region Users

// RenderVolumeUsers renders [corev1.Volume] containing users contents
func RenderVolumeUsers(cluster *values.SlurmCluster) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeUsersName,
		VolumeSource: utils.MustGetBy(
			cluster.VolumeSources,
			*cluster.NodeController.VolumeUsers.VolumeSourceName,
			func(s slurmv1.VolumeSource) string { return s.Name },
		).VolumeSource,
	}
}

// RenderVolumeMountUsers renders [corev1.VolumeMount] defining the mounting path for users contents
func RenderVolumeMountUsers() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeSpoolName,
		MountPath: consts.VolumeUsersMountPath,
	}
}

// endregion Users

// region Spool

// RenderVolumeSpool renders [corev1.Volume] containing spool contents
func RenderVolumeSpool(cluster *values.SlurmCluster) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeSpoolName,
		VolumeSource: utils.MustGetBy(
			cluster.VolumeSources,
			*cluster.NodeController.VolumeSpool.VolumeSourceName,
			func(s slurmv1.VolumeSource) string { return s.Name },
		).VolumeSource,
	}
}

// RenderVolumeMountSpool renders [corev1.VolumeMount] defining the mounting path for spool contents
func RenderVolumeMountSpool(directory string) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeSpoolName,
		MountPath: naming.BuildVolumeMountSpoolPath(directory),
	}
}

// endregion Spool
