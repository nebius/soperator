package common

import (
	"fmt"

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

// region Slurm configs

// RenderVolumeSlurmConfigs renders [corev1.Volume] containing Slurm config files
func RenderVolumeSlurmConfigs(cluster *values.SlurmCluster) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSlurmConfigs,
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
		Name:      consts.VolumeNameSlurmConfigs,
		MountPath: consts.VolumeMountPathSlurmConfigs,
		ReadOnly:  true,
	}
}

// endregion Slurm configs

// region Spool

func RenderVolumeNameSpool(componentType consts.ComponentType) string {
	return fmt.Sprintf("%s-%s", componentType.String(), consts.VolumeNameSpool)
}

// RenderVolumeSpool renders [corev1.Volume] containing spool contents
func RenderVolumeSpool(componentType consts.ComponentType, cluster *values.SlurmCluster) corev1.Volume {
	return corev1.Volume{
		Name: RenderVolumeNameSpool(componentType),
		VolumeSource: utils.MustGetBy(
			cluster.VolumeSources,
			*cluster.NodeController.VolumeSpool.VolumeSourceName,
			func(s slurmv1.VolumeSource) string { return s.Name },
		).VolumeSource,
	}
}

// RenderVolumeMountSpool renders [corev1.VolumeMount] defining the mounting path for spool contents
func RenderVolumeMountSpool(componentType consts.ComponentType, directory string) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      RenderVolumeNameSpool(componentType),
		MountPath: naming.BuildVolumeMountSpoolPath(directory),
	}
}

// endregion Spool

// region Jail

// RenderVolumeJail renders [corev1.Volume] containing jail contents
func RenderVolumeJail(cluster *values.SlurmCluster) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameJail,
		VolumeSource: utils.MustGetBy(
			cluster.VolumeSources,
			*cluster.NodeController.VolumeJail.VolumeSourceName,
			func(s slurmv1.VolumeSource) string { return s.Name },
		).VolumeSource,
	}
}

// RenderVolumeMountJail renders [corev1.VolumeMount] defining the mounting path for jail contents
func RenderVolumeMountJail() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameJail,
		MountPath: consts.VolumeMountPathJail,
	}
}

// endregion Jail

// region Munge

// RenderVolumeMungeSocket renders [corev1.Volume] containing munge socket contents
func RenderVolumeMungeSocket() corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameMungeSocket,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

// RenderVolumeMountMungeSocket renders [corev1.VolumeMount] defining the mounting path for munge socket
func RenderVolumeMountMungeSocket() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameMungeSocket,
		MountPath: consts.VolumeMountPathMungeSocket,
	}
}

// RenderVolumeMungeKey renders [corev1.Volume] containing munge key file
func RenderVolumeMungeKey(cluster *values.SlurmCluster) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameMungeKey,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: cluster.Secrets.MungeKey.Name,
				Items: []corev1.KeyToPath{
					{
						Key:  cluster.Secrets.MungeKey.Key,
						Path: consts.SecretMungeKeyFileName,
						Mode: &consts.SecretMungeKeyFileMode,
					},
				},
			},
		},
	}
}

// RenderVolumeMountMungeKey renders [corev1.VolumeMount] defining the mounting path for munge key file
func RenderVolumeMountMungeKey() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameMungeKey,
		MountPath: consts.VolumeMountPathMungeKey,
		ReadOnly:  true,
	}
}

// endregion Munge
