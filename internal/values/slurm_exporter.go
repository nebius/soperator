package values

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

type SlurmExporter struct {
	slurmv1.SlurmNode

	Enabled              bool
	UseSoperatorExporter bool
	PodMonitorConfig     slurmv1.PodMonitorConfig

	slurmv1.ExporterContainer
	ContainerMunge       Container
	CustomInitContainers []corev1.Container

	VolumeJail  slurmv1.NodeVolume
	Maintenance *consts.MaintenanceMode
}

func buildSlurmExporterFrom(maintenance *consts.MaintenanceMode, exporter *slurmv1.SlurmExporter) SlurmExporter {
	return SlurmExporter{
		SlurmNode:            *exporter.SlurmNode.DeepCopy(),
		Enabled:              exporter.Enabled,
		UseSoperatorExporter: exporter.UseSoperatorExporter,
		PodMonitorConfig:     *exporter.PodMonitorConfig.DeepCopy(),
		ExporterContainer:    *exporter.Exporter.DeepCopy(),
		ContainerMunge: buildContainerFrom(
			exporter.Munge,
			consts.ContainerNameMunge,
		),
		CustomInitContainers: exporter.CustomInitContainers,
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To(consts.VolumeNameJail),
		},
		Maintenance: maintenance,
	}
}
