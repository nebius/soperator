package values

import (
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

// SlurmWorker contains the data needed to deploy and reconcile the Slurm Workers
type SConfigController struct {
	slurmv1.SlurmNode

	Container  Container
	VolumeJail slurmv1.NodeVolume

	Maintenance         consts.MaintenanceMode
	JailSlurmConfigPath string
}

func buildSConfigControllerFrom(
	node slurmv1.SlurmNode,
	container slurmv1.NodeContainer,
	maintenance consts.MaintenanceMode,
	jailSlurmConfigPath string,
) SConfigController {
	containerSConfigController := buildContainerFrom(
		container,
		consts.ContainerNameSConfigController,
	)
	if jailSlurmConfigPath == "" {
		jailSlurmConfigPath = consts.DefaultPathEtcSlurm
	}

	res := SConfigController{
		SlurmNode: node,
		Container: containerSConfigController,
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To(consts.VolumeNameJail),
		},
		Maintenance:         maintenance,
		JailSlurmConfigPath: jailSlurmConfigPath,
	}

	return res
}
