package values

import (
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

type SlurmExporter struct {
	slurmv1.SlurmNode

	Enabled          bool
	PodMonitorConfig slurmv1.PodMonitorConfig

	slurmv1.ExporterContainer
	ContainerMunge Container

	VolumeJail slurmv1.NodeVolume
}

func buildSlurmExporterFrom(exporter *slurmv1.SlurmExporter) SlurmExporter {
	return SlurmExporter{
		SlurmNode:         *exporter.SlurmNode.DeepCopy(),
		Enabled:           exporter.Enabled,
		PodMonitorConfig:  *exporter.PodMonitorConfig.DeepCopy(),
		ExporterContainer: *exporter.Exporter.DeepCopy(),
		ContainerMunge: buildContainerFrom(
			exporter.Munge,
			consts.ContainerNameMunge,
		),
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To(consts.VolumeNameJail),
		},
	}
}
