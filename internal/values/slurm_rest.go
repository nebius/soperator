package values

import (
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// SlurmREST contains the data needed to deploy and reconcile the Slurm REST API
type SlurmREST struct {
	slurmv1.SlurmNode

	Enabled       bool
	ContainerREST Container
	Service       Service
}

func buildRestFrom(clusterName string, rest *slurmv1.SlurmRest) SlurmREST {
	containerREST := buildContainerFrom(
		rest.SlurmRestNode,
		consts.ContainerNameREST,
	)
	if containerREST.Port == 0 {
		containerREST.Port = consts.DefaultRESTPort
	}

	return SlurmREST{
		SlurmNode:     *rest.SlurmNode.DeepCopy(),
		Enabled:       rest.Enabled,
		ContainerREST: containerREST,
		Service:       buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeREST, clusterName)),
	}
}
