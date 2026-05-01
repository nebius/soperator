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
	env := []corev1.EnvVar{
		{Name: "SLURM_EXPORTER_CLUSTER_NAMESPACE", Value: clusterValues.Namespace},
		{Name: "SLURM_EXPORTER_CLUSTER_NAME", Value: clusterValues.Name},
		{Name: "SLURM_EXPORTER_SLURM_API_SERVER", Value: rest.GetServiceURL(clusterValues.Namespace, &clusterValues.NodeRest)},
		{Name: "SLURM_EXPORTER_COLLECTION_INTERVAL", Value: string(clusterValues.SlurmExporter.CollectionInterval)},
	}
	if clusterValues.SlurmExporter.JobSources != "" {
		env = append(env, corev1.EnvVar{Name: "SLURM_EXPORTER_JOB_SOURCES", Value: clusterValues.SlurmExporter.JobSources})
	}
	if clusterValues.SlurmExporter.AccountingJobMode != "" {
		env = append(env, corev1.EnvVar{Name: "SLURM_EXPORTER_ACCOUNTING_JOB_MODE", Value: clusterValues.SlurmExporter.AccountingJobMode})
	}

	return corev1.Container{
		Name:    consts.ContainerNameExporter,
		Image:   clusterValues.SlurmExporter.Container.Image,
		Command: clusterValues.SlurmExporter.Container.Command,
		// Keep existing CLI args for backward compatibility
		// NOTE: New parameters should ONLY be added to Env, not Args, to maintain forward compatibility
		// TODO: Remove Args in 2026.
		Args: []string{
			fmt.Sprintf("--cluster-namespace=%s", clusterValues.Namespace),
			fmt.Sprintf("--cluster-name=%s", clusterValues.Name),
			fmt.Sprintf("--slurm-api-server=%s", rest.GetServiceURL(clusterValues.Namespace, &clusterValues.NodeRest)),
		},
		// All new parameters MUST be added here, not to Args
		Env:             env,
		ImagePullPolicy: clusterValues.SlurmExporter.Container.ImagePullPolicy,
		Ports: []corev1.ContainerPort{
			{
				Name:          consts.ContainerPortNameExporter,
				ContainerPort: consts.ContainerPortExporter,
			},
			{
				Name:          consts.ContainerPortNameMonitoring,
				ContainerPort: consts.ContainerPortMonitoring,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: clusterValues.SlurmExporter.Container.Resources,
			Limits:   common.CopyNonCPUResources(clusterValues.SlurmExporter.Container.Resources),
		},
		LivenessProbe:  clusterValues.SlurmExporter.Container.LivenessProbe,
		ReadinessProbe: clusterValues.SlurmExporter.Container.ReadinessProbe,
		VolumeMounts:   []corev1.VolumeMount{},
	}
}
