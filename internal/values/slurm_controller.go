package values

import (
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// SlurmController contains the data needed to deploy and reconcile the Slurm Controllers
type SlurmController struct {
	slurmv1.SlurmNode

	Service     Service
	StatefulSet StatefulSet
	VolumeUsers slurmv1.NodeVolume
	VolumeSpool slurmv1.NodeVolume
}

func buildSlurmControllerFrom(cluster *slurmv1.SlurmCluster) SlurmController {
	return SlurmController{
		SlurmNode: *cluster.Spec.SlurmNodes.Controller.SlurmNode.DeepCopy(),
		Service: buildServiceFrom(
			cluster.Spec.SlurmNodes.Controller.Slurmctld,
			naming.BuildServiceName(consts.ComponentTypeController, cluster.Name),
		),
		StatefulSet: buildStatefulSetFrom(
			naming.BuildStatefulSetName(consts.ComponentTypeController, cluster.Name),
			cluster.Spec.SlurmNodes.Controller.SlurmNode.Size,
		),
		VolumeUsers: *cluster.Spec.SlurmNodes.Controller.Volumes.Users.DeepCopy(),
		VolumeSpool: *cluster.Spec.SlurmNodes.Controller.Volumes.Spool.DeepCopy(),
	}
}
