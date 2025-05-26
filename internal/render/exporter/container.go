package exporter

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func renderContainerExporter(
	exporterValues values.SlurmExporter,
	clusterNamespace string,
	clusterName string,
	slurmAPIServer string,
) corev1.Container {
	args := []string{
		fmt.Sprintf("--cluster-namespace=%s", clusterNamespace),
		fmt.Sprintf("--cluster-name=%s", clusterName),
		fmt.Sprintf("--slurm-api-server=%s", slurmAPIServer),
	}
	return corev1.Container{
		Name:            consts.ContainerNameExporter,
		Image:           exporterValues.NodeContainer.Image,
		Command:         exporterValues.NodeContainer.Command,
		Args:            args,
		ImagePullPolicy: exporterValues.NodeContainer.ImagePullPolicy,
		Ports: []corev1.ContainerPort{
			{
				Name:          consts.ContainerPortNameExporter,
				ContainerPort: consts.ContainerPortExporter,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: exporterValues.NodeContainer.Resources,
			Limits:   common.CopyNonCPUResources(exporterValues.NodeContainer.Resources),
		},
	}
}
