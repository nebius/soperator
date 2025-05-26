package values

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

type SlurmExporter struct {
	slurmv1.SlurmNode

	Enabled bool

	PodMonitorConfig slurmv1.PodMonitorConfig

	// ExporterContainer is a pair of NodeContainer + PodTemplateNameRef.
	// Deprecated: will be removed when Slurm Exporter will be replaced with Soperator Exporter.
	slurmv1.ExporterContainer

	// ContainerMunge is a container that runs Munge daemon.
	// Deprecated: will be removed when Slurm Exporter will be replaced with Soperator Exporter.
	ContainerMunge Container

	CustomInitContainers []corev1.Container

	// VolumeJail is a volume that is used to mount the jail directory for the Slurm Exporter.
	// Deprecated: will be removed when Slurm Exporter will be replaced with Soperator Exporter.
	VolumeJail slurmv1.NodeVolume

	Maintenance *consts.MaintenanceMode

	// NodeContainer represents the main container for the Soperator Exporter.
	NodeContainer slurmv1.NodeContainer

	// PodTemplatePatchNameRef is name of pod template to patch the Soperator Exporter pods.
	PodTemplatePatchNameRef *string
}

func buildSlurmExporterFrom(maintenance *consts.MaintenanceMode, exporter *slurmv1.SlurmExporter) SlurmExporter {
	return SlurmExporter{
		SlurmNode:         *exporter.SlurmNode.DeepCopy(),
		Enabled:           exporter.Enabled,
		PodMonitorConfig:  *exporter.PodMonitorConfig.DeepCopy(),
		ExporterContainer: *exporter.Exporter.DeepCopy(),
		ContainerMunge: buildContainerFrom(
			exporter.Munge,
			consts.ContainerNameMunge,
		),
		CustomInitContainers: exporter.CustomInitContainers,
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To(consts.VolumeNameJail),
		},
		Maintenance:             maintenance,
		NodeContainer:           *exporter.NodeContainer.DeepCopy(),
		PodTemplatePatchNameRef: exporter.PodTemplatePatchNameRef,
	}
}
