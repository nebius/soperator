package common

import (
	"errors"
	"fmt"
	"path"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// region PVC template

func RenderVolumeClaimTemplates(
	componentType consts.ComponentType,
	namespace,
	clusterName string,
	pvcTemplateSpecs []values.PVCTemplateSpec,
) []corev1.PersistentVolumeClaim {
	var res []corev1.PersistentVolumeClaim
	for _, template := range pvcTemplateSpecs {
		if template.Spec == nil {
			continue
		}
		res = append(
			res,
			renderVolumeClaimTemplate(componentType, namespace, clusterName, template.Name, *template.Spec),
		)
	}
	return res
}

func renderVolumeClaimTemplate(
	componentType consts.ComponentType,
	namespace,
	clusterName,
	pvcName string,
	pvcClaimSpec corev1.PersistentVolumeClaimSpec,
) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
			Labels:    RenderLabels(componentType, clusterName),
		},
		Spec: pvcClaimSpec,
	}
}

// endregion PVC template

// region Slurm configs

// RenderVolumeSlurmConfigs renders [corev1.Volume] containing Slurm config files
func RenderVolumeSlurmConfigs(clusterName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSlurmConfigs,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: naming.BuildConfigMapSlurmConfigsName(clusterName),
				},
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

// RenderVolumeSpoolFromSource renders [corev1.Volume] containing spool contents
func RenderVolumeSpoolFromSource(
	componentType consts.ComponentType,
	sources []slurmv1.VolumeSource,
	sourceName string,
) corev1.Volume {
	return RenderVolumeFromSource(sources, sourceName, RenderVolumeNameSpool(componentType))
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

// RenderVolumeJailFromSource renders [corev1.Volume] containing jail contents
func RenderVolumeJailFromSource(sources []slurmv1.VolumeSource, sourceName string) corev1.Volume {
	return RenderVolumeFromSource(sources, sourceName, consts.VolumeNameJail)
}

// RenderVolumeMountJail renders [corev1.VolumeMount] defining the mounting path for jail contents
func RenderVolumeMountJail() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameJail,
		MountPath: consts.VolumeMountPathJail,
	}
}

// endregion Jail

// region JailSnapshot

// RenderVolumeJailSnapshotFromSource renders [corev1.Volume] containing initial jail snapshot contents
func RenderVolumeJailSnapshotFromSource(sources []slurmv1.VolumeSource, sourceName string) corev1.Volume {
	return RenderVolumeFromSource(sources, sourceName, consts.VolumeNameJailSnapshot)
}

// RenderVolumeMountJailSnapshot renders [corev1.VolumeMount] defining the mounting path for jail snapshot contents
func RenderVolumeMountJailSnapshot() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameJailSnapshot,
		MountPath: consts.VolumeMountPathJailSnapshot,
	}
}

// endregion JailSnapshot

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
func RenderVolumeMungeKey(clusterName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameMungeKey,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: naming.BuildSecretMungeKeyName(clusterName),
				Items: []corev1.KeyToPath{
					{
						Key:  consts.SecretMungeKeyFileName,
						Path: consts.SecretMungeKeyFileName,
						Mode: ptr.To(consts.SecretMungeKeyFileMode),
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

// region JailSubMounts

func RenderVolumeMountsForJailSubMounts(subMounts []slurmv1.NodeVolumeJailSubMount) []corev1.VolumeMount {
	var res []corev1.VolumeMount
	for _, subMount := range subMounts {
		res = append(res, RenderVolumeMountJailSubMount(subMount))
	}
	return res
}

func RenderVolumeMountJailSubMount(subMount slurmv1.NodeVolumeJailSubMount) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      subMount.Name,
		MountPath: path.Join(consts.VolumeMountPathJailUpper, subMount.MountPath),
	}
}

// endregion JailSubMounts

func RenderVolumeFromSource(sources []slurmv1.VolumeSource, sourceName, volumeName string) corev1.Volume {
	return corev1.Volume{
		Name: volumeName,
		VolumeSource: utils.MustGetBy(
			sources,
			sourceName,
			func(s slurmv1.VolumeSource) string { return s.Name },
		).VolumeSource,
	}
}

func AddVolumeOrSpec(
	volumeSourceName *string,
	volumeCreator func(sourceName string) corev1.Volume,
	pvcTemplateSpec *corev1.PersistentVolumeClaimSpec,
	specName string,
) (volumes []corev1.Volume, pvcTemplateSpecs []values.PVCTemplateSpec, err error) {
	if (volumeSourceName != nil && pvcTemplateSpec != nil) || (volumeSourceName == nil && pvcTemplateSpec == nil) {
		return nil, nil, errors.New("only one of VolumeSourceName or VolumeClaimTemplateSpec should be set")
	}

	if volumeSourceName != nil {
		volumes = append(volumes, volumeCreator(*volumeSourceName))
	}
	if pvcTemplateSpec != nil {
		pvcTemplateSpecs = append(pvcTemplateSpecs, values.PVCTemplateSpec{Name: specName, Spec: pvcTemplateSpec})
	}

	return volumes, pvcTemplateSpecs, nil
}

// region security limits

// RenderVolumeSecurityLimits renders [corev1.Volume] containing security limits config contents
func RenderVolumeSecurityLimits(clusterName string, componentType consts.ComponentType) corev1.Volume {
	return corev1.Volume{
		Name: consts.VolumeNameSecurityLimits,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: naming.BuildConfigMapSecurityLimitsName(componentType, clusterName),
				},
			},
		},
	}
}

// RenderVolumeMountSecurityLimits renders [corev1.VolumeMount] defining the mounting path for security limits config
func RenderVolumeMountSecurityLimits() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameSecurityLimits,
		MountPath: consts.VolumeMountPathSecurityLimits,
		SubPath:   consts.VolumeMountSubPathSecurityLimits,
	}
}

// endregion security limits
