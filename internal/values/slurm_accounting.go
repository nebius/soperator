package values

import (
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// SlurmAccounting contains the data needed to deploy and reconcile the Slurm Controllers
type SlurmAccounting struct {
	slurmv1.SlurmNode

	Enabled bool

	ContainerAccounting Container
	ContainerMunge      Container

	Service    Service
	Deployment Deployment
	ExternalDB slurmv1.ExternalDB
	MariaDb    slurmv1.MariaDbOperator
	VolumeJail slurmv1.NodeVolume
}

func buildAccountingFrom(clusterName string, accounting *slurmv1.SlurmNodeAccounting) SlurmAccounting {
	containerAcc := buildContainerFrom(
		accounting.Slurmdbd,
		consts.ContainerNameAccounting,
	)
	if containerAcc.Port == 0 {
		containerAcc.Port = consts.DefaultAccountingPort
	}
	return SlurmAccounting{
		SlurmNode:           *accounting.SlurmNode.DeepCopy(),
		Enabled:             accounting.Enabled,
		ContainerAccounting: containerAcc,
		ContainerMunge: buildContainerFrom(
			accounting.Munge,
			consts.ContainerNameMunge,
		),
		Service: buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeAccounting, clusterName)),
		Deployment: buildDeploymentFrom(
			naming.BuildDeploymentName(consts.ComponentTypeAccounting),
		),
		ExternalDB: accounting.ExternalDB,
		MariaDb:    accounting.MariaDbOperator,
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To(consts.VolumeNameJail),
		},
	}
}
