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

	Maintenance consts.MaintenanceMode

	RunAsUid                *int64
	RunAsGid                *int64
	ReconfigurePollInterval *string
	ReconfigureWaitTimeout  *string
}

func buildSConfigControllerFrom(
	node slurmv1.SlurmNode,
	container slurmv1.NodeContainer,
	maintenance consts.MaintenanceMode,
	runAsUid *int64,
	runAsGid *int64,
	reconfigurePollInterval *string,
	reconfigureWaitTimeout *string,
) SConfigController {
	containerSConfigController := buildContainerFrom(
		container,
		consts.ContainerNameSConfigController,
	)

	res := SConfigController{
		SlurmNode: node,
		Container: containerSConfigController,
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To(consts.VolumeNameJail),
		},
		Maintenance:             maintenance,
		RunAsUid:                runAsUid,
		RunAsGid:                runAsGid,
		ReconfigurePollInterval: reconfigurePollInterval,
		ReconfigureWaitTimeout:  reconfigureWaitTimeout,
	}

	return res
}
