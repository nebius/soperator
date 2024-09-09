package prometheus

import (
	"path"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderContainerExporter(containerParams *values.SlurmExporter) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameExporter,
		Image:           *containerParams.ImageSlurmExporter,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports: []corev1.ContainerPort{
			{
				Name:          consts.ContainerPortNameExporter,
				ContainerPort: consts.ContainerPortExporter,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: containerParams.ResourcesSlurmExporter,
			// We are do not want to use limits for cpu
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: *containerParams.ResourcesSlurmExporter.Memory(),
			},
		},
		Env: []corev1.EnvVar{
			{
				Name:  "SLURM_CONF",
				Value: path.Join(consts.VolumeMountPathSlurmConfigs, "slurm.conf"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			common.RenderVolumeMountMungeSocket(),
			common.RenderVolumeMountSlurmConfigs(),
		},
	}
}
