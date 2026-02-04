package values

import (
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// SlurmREST contains the data needed to deploy and reconcile the Slurm REST API
type SlurmREST struct {
	slurmv1.SlurmNode

	Enabled              bool
	ThreadCount          *int32
	MaxConnections       *int32
	ContainerREST        Container
	CustomInitContainers []slurmv1.InitContainer
	Service              Service
	VolumeJail           slurmv1.NodeVolume
	Maintenance          *consts.MaintenanceMode
}

func buildRestFrom(clusterName string, maintenance *consts.MaintenanceMode, rest *slurmv1.SlurmRest) SlurmREST {
	containerREST := buildContainerFrom(
		rest.SlurmRestNode,
		consts.ContainerNameREST,
	)
	if containerREST.Port == 0 {
		containerREST.Port = consts.DefaultRESTPort
	}

	return SlurmREST{
		SlurmNode:            *rest.SlurmNode.DeepCopy(),
		Enabled:              rest.Enabled,
		ThreadCount:          rest.ThreadCount,
		MaxConnections:       rest.MaxConnections,
		ContainerREST:        containerREST,
		CustomInitContainers: rest.CustomInitContainers,
		Service:              buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeREST, clusterName)),
		Maintenance:          maintenance,
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To(consts.VolumeNameJail),
		},
	}
}
