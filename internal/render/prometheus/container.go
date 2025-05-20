package prometheus

import (
	"fmt"
	"path"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderContainerExporter(
	containerParams *values.SlurmExporter, clusterNamespace, clusterName, slurmAPIServer string,
) corev1.Container {
	// Create a copy of the container's limits and add non-CPU resources from Requests
	limits := common.CopyNonCPUResources(containerParams.ExporterContainer.Resources)
	args := containerParams.Args
	var env []corev1.EnvVar
	var volumeMounts []corev1.VolumeMount
	if containerParams.UseSoperatorExporter {
		args = []string{
			fmt.Sprintf("--cluster-namespace=%s", clusterNamespace),
			fmt.Sprintf("--cluster-name=%s", clusterName),
			fmt.Sprintf("--slurm-api-server=%s", slurmAPIServer),
		}
	} else {
		env = []corev1.EnvVar{
			{
				Name:  "SLURM_CONF",
				Value: path.Join(consts.VolumeMountPathSlurmConfigs, "slurm.conf"),
			},
		}
		volumeMounts = []corev1.VolumeMount{
			common.RenderVolumeMountMungeSocket(),
			common.RenderVolumeMountSlurmConfigs(),
		}
	}
	return corev1.Container{
		Name:            consts.ContainerNameExporter,
		Image:           containerParams.ExporterContainer.Image,
		Command:         containerParams.Command,
		Args:            args,
		ImagePullPolicy: containerParams.ExporterContainer.ImagePullPolicy,
		Ports: []corev1.ContainerPort{
			{
				Name:          consts.ContainerPortNameExporter,
				ContainerPort: consts.ContainerPortExporter,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: containerParams.ExporterContainer.Resources,
			// We do not want to use limits for cpu
			Limits: limits,
		},
		Env:          env,
		VolumeMounts: volumeMounts,
	}
}
