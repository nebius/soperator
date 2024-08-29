package values

import (
	"k8s.io/utils/ptr"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

type SlurmExporter struct {
	slurmv1.MetricsPrometheus

	slurmv1.SlurmNode

	Name string

	ContainerMunge Container

	VolumeJail slurmv1.NodeVolume
}

func buildSlurmExporterFrom(
	clusterName string,
	telemetry *slurmv1.Telemetry,
	controller *slurmv1.SlurmNodeController,
) SlurmExporter {
	return SlurmExporter{
		MetricsPrometheus: *telemetry.Prometheus.DeepCopy(),
		SlurmNode:         *controller.SlurmNode.DeepCopy(),
		Name:              naming.BuildSlurmExporterName(clusterName),
		ContainerMunge: buildContainerFrom(
			controller.Munge,
			consts.ContainerNameMunge,
		),
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To(consts.VolumeNameJail),
		},
	}
}
