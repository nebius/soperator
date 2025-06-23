package exporter

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/render/rest"
	"nebius.ai/slurm-operator/internal/values"
)

func renderContainerExporter(clusterValues *values.SlurmCluster) corev1.Container {
	return corev1.Container{
		Name:    consts.ContainerNameExporter,
		Image:   clusterValues.SlurmExporter.Container.Image,
		Command: clusterValues.SlurmExporter.Container.Command,
		Args: []string{
			fmt.Sprintf("--cluster-namespace=%s", clusterValues.Namespace),
			fmt.Sprintf("--cluster-name=%s", clusterValues.Name),
			fmt.Sprintf("--slurm-api-server=%s", rest.GetServiceURL(clusterValues.Namespace, &clusterValues.NodeRest)),
		},
		ImagePullPolicy: clusterValues.SlurmExporter.Container.ImagePullPolicy,
		Ports: []corev1.ContainerPort{
			{
				Name:          consts.ContainerPortNameExporter,
				ContainerPort: consts.ContainerPortExporter,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: clusterValues.SlurmExporter.Container.Resources,
			Limits:   common.CopyNonCPUResources(clusterValues.SlurmExporter.Container.Resources),
		},
	}
}
