package values

import (
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
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

	CustomInitContainers []slurmv1.InitContainer

	// VolumeJail is a volume that is used to mount the jail directory for the Soperator Exporter.
	//
	// Deprecated: not required anymore.
	VolumeJail slurmv1.NodeVolume

	Maintenance *consts.MaintenanceMode

	// Container represents the main container for the Soperator Exporter.
	Container slurmv1.NodeContainer

	// CollectionInterval specifies how often to collect metrics from SLURM APIs
	CollectionInterval prometheusv1.Duration

	// ServiceAccountName is the ServiceAccount to be used by exporter pods.
	ServiceAccountName string
}

func buildSlurmExporterFrom(maintenance *consts.MaintenanceMode, exporter *slurmv1.SlurmExporter) SlurmExporter {
	enabled := false
	if exporter.Enabled != nil {
		enabled = *exporter.Enabled
	}

	return SlurmExporter{
		SlurmNode:         *exporter.SlurmNode.DeepCopy(),
		Enabled:           enabled,
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
		Maintenance:        maintenance,
		Container:          *exporter.ExporterContainer.DeepCopy(),
		CollectionInterval: exporter.CollectionInterval,
		ServiceAccountName: exporter.ServiceAccountName,
	}
}
