package sconfigcontroller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func renderContainerSConfigController(
	clusterNamespace, clusterName, slurmAPIServer, jailConfigPath string, container values.Container) corev1.Container {
	// Create a copy of the container's limits and add non-CPU resources from Requests
	limits := common.CopyNonCPUResources(container.Resources)

	jailMount := common.RenderVolumeMountJail()

	return corev1.Container{
		Name:            consts.ContainerNameSConfigController,
		Image:           container.Image,
		ImagePullPolicy: container.ImagePullPolicy,
		VolumeMounts: []corev1.VolumeMount{
			jailMount,
		},
		Resources: corev1.ResourceRequirements{
			Limits:   limits,
			Requests: container.Resources,
		},
		Command: []string{
			"/usr/bin/sconfigcontroller",
		},
		Args: []string{
			fmt.Sprintf("--cluster-namespace=%s", clusterNamespace),
			fmt.Sprintf("--cluster-name=%s", clusterName),
			fmt.Sprintf("--configs-path=%s", jailConfigPath),
			fmt.Sprintf("--jail-path=%s", jailMount.MountPath),
			fmt.Sprintf("--slurmapiserver=%s", slurmAPIServer),
			"--leader-elect",
		},
	}
}
