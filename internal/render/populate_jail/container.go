package populate_jail

import (
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func renderContainerPopulateJail(clusterType consts.ClusterType, populateJail *values.PopulateJail) corev1.Container {
	volumeMounts := []corev1.VolumeMount{
		common.RenderVolumeMountJail(),
	}
	if populateJail.JailSnapshotVolume != nil {
		volumeMounts = append(volumeMounts, common.RenderVolumeMountJailSnapshot())
	}
	overwriteEnv := "0"
	if populateJail.Overwrite || check.IsModeDownscaleAndOverwritePopulate(populateJail.Maintenance) {
		overwriteEnv = "1"
	}

	return corev1.Container{
		Name:            populateJail.ContainerPopulateJail.Name,
		Image:           populateJail.ContainerPopulateJail.Image,
		ImagePullPolicy: populateJail.ContainerPopulateJail.ImagePullPolicy,
		Env: []corev1.EnvVar{
			{
				Name:  "OVERWRITE",
				Value: overwriteEnv},
			{
				Name:  "SLURM_CLUSTER_TYPE",
				Value: clusterType.String(),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				},
			},
			AppArmorProfile: common.ParseAppArmorProfile(populateJail.ContainerPopulateJail.AppArmorProfile),
		},
		VolumeMounts: volumeMounts,
	}
}
