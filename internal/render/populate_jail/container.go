package populate_jail

import (
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func renderContainerPopulateJail(populateJail *values.PopulateJail) corev1.Container {
	volumeMounts := []corev1.VolumeMount{
		common.RenderVolumeMountJail(),
	}
	if populateJail.JailSnapshotVolume != nil {
		volumeMounts = append(volumeMounts, common.RenderVolumeMountJailSnapshot())
	}
	return corev1.Container{
		Name:            populateJail.ContainerPopulateJail.Name,
		Image:           populateJail.ContainerPopulateJail.Image,
		ImagePullPolicy: corev1.PullAlways, // TODO use digest and set to corev1.PullIfNotPresent
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				},
			},
		},
		VolumeMounts: volumeMounts,
	}
}
