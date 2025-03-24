package values

import (
	corev1 "k8s.io/api/core/v1"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// SlurmController contains the data needed to deploy and reconcile the Slurm Controllers
type SlurmController struct {
	slurmv1.SlurmNode

	ContainerSlurmctld   Container
	ContainerMunge       Container
	CustomInitContainers []corev1.Container

	Service     Service
	StatefulSet StatefulSet

	VolumeSpool        slurmv1.NodeVolume
	VolumeJail         slurmv1.NodeVolume
	CustomVolumeMounts []slurmv1.NodeVolumeMount
	Maintenance        *consts.MaintenanceMode
}

func buildSlurmControllerFrom(clusterName string, maintenance *consts.MaintenanceMode, controller *slurmv1.SlurmNodeController) SlurmController {
	res := SlurmController{
		SlurmNode: *controller.SlurmNode.DeepCopy(),
		ContainerSlurmctld: buildContainerFrom(
			controller.Slurmctld,
			consts.ContainerNameSlurmctld,
		),
		ContainerMunge: buildContainerFrom(
			controller.Munge,
			consts.ContainerNameMunge,
		),
		CustomInitContainers: controller.CustomInitContainers,
		Service:              buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeController, clusterName)),
		StatefulSet: buildStatefulSetFrom(
			naming.BuildStatefulSetName(consts.ComponentTypeController, clusterName),
			controller.SlurmNode.Size,
		),
		VolumeSpool:        *controller.Volumes.Spool.DeepCopy(),
		VolumeJail:         *controller.Volumes.Jail.DeepCopy(),
		CustomVolumeMounts: controller.Volumes.CustomMounts,
		Maintenance:        maintenance,
	}

	for _, customVolumeMount := range controller.Volumes.CustomMounts {
		customMount := *customVolumeMount.DeepCopy()
		res.CustomVolumeMounts = append(res.CustomVolumeMounts, customMount)
	}

	return res
}
