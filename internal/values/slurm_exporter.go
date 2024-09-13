package values

import (
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

type SlurmExporter struct {
	slurmv1.SlurmNode

	Name             string
	Enabled          bool
	PodMonitorConfig slurmv1.PodMonitorConfig

	slurmv1.ExporterContainer
	ContainerMunge Container

	VolumeJail slurmv1.NodeVolume
}

func buildSlurmExporterFrom(clusterName string, exporter *slurmv1.SlurmExporter) SlurmExporter {
	return SlurmExporter{
		SlurmNode:         *exporter.SlurmNode.DeepCopy(),
		Name:              naming.BuildSlurmExporterName(clusterName),
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
