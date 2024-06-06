package values

import (
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// SlurmController contains the data needed to deploy and reconcile the Slurm Controllers
type SlurmController struct {
	slurmv1.SlurmNode

	ContainerSlurmctld Container
	ContainerMunge     Container

	Service     Service
	StatefulSet StatefulSet

	VolumeSpool slurmv1.NodeVolume
	VolumeJail  slurmv1.NodeVolume
}

func buildSlurmControllerFrom(cluster *slurmv1.SlurmCluster) SlurmController {
	return SlurmController{
		SlurmNode: *cluster.Spec.SlurmNodes.Controller.SlurmNode.DeepCopy(),
		ContainerSlurmctld: buildContainerFrom(
			cluster.Spec.SlurmNodes.Controller.Slurmctld,
			consts.ContainerSlurmctldName,
		),
		ContainerMunge: buildContainerFrom(
			cluster.Spec.SlurmNodes.Controller.Munge,
			consts.ContainerMungeName,
		),
		Service: buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeController, cluster.Name)),
		StatefulSet: buildStatefulSetFrom(
			naming.BuildStatefulSetName(consts.ComponentTypeController, cluster.Name),
			cluster.Spec.SlurmNodes.Controller.SlurmNode.Size,
		),
		VolumeSpool: *cluster.Spec.SlurmNodes.Controller.Volumes.Spool.DeepCopy(),
		VolumeJail:  *cluster.Spec.SlurmNodes.Controller.Volumes.Jail.DeepCopy(),
	}
}
