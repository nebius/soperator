package sconfigcontroller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func renderContainerSConfigController(
	clusterNamespace, clusterName, slurmAPIServer string,
	sConfigController *values.SConfigController,
) corev1.Container {
	// Create a copy of the container's limits and add non-CPU resources from Requests
	limits := common.CopyNonCPUResources(sConfigController.Container.Resources)

	jailMount := common.RenderVolumeMountJail()

	args := []string{
		fmt.Sprintf("--cluster-namespace=%s", clusterNamespace),
		fmt.Sprintf("--cluster-name=%s", clusterName),
		fmt.Sprintf("--jail-path=%s", jailMount.MountPath),
		fmt.Sprintf("--slurmapiserver=%s", slurmAPIServer),
		"--leader-elect",
	}

	// Add optional parameters if they are specified
	if sConfigController.ReconfigurePollInterval != nil {
		args = append(args, fmt.Sprintf("--reconfigure-poll-interval=%s", *sConfigController.ReconfigurePollInterval))
	}
	if sConfigController.ReconfigureWaitTimeout != nil {
		args = append(args, fmt.Sprintf("--reconfigure-wait-timeout=%s", *sConfigController.ReconfigureWaitTimeout))
	}

	return corev1.Container{
		Name:            consts.ContainerNameSConfigController,
		Image:           sConfigController.Container.Image,
		ImagePullPolicy: sConfigController.Container.ImagePullPolicy,
		VolumeMounts: []corev1.VolumeMount{
			jailMount,
		},
		Resources: corev1.ResourceRequirements{
			Limits:   limits,
			Requests: sConfigController.Container.Resources,
		},
		Command: []string{
			"/usr/bin/sconfigcontroller",
		},
		Args: args,
	}
}
