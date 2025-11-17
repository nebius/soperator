package values

import (
	corev1 "k8s.io/api/core/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// SlurmController contains the data needed to deploy and reconcile the Slurm Controllers
type SlurmController struct {
	K8sNodeFilterName    string
	CustomInitContainers []corev1.Container
	HostUsers            *bool

	ContainerSlurmctld Container
	ContainerMunge     Container

	Service     Service
	StatefulSet StatefulSet

	VolumeSpool        slurmv1.NodeVolume
	VolumeJail         slurmv1.NodeVolume
	CustomVolumeMounts []slurmv1.NodeVolumeMount
	Maintenance        *consts.MaintenanceMode
	PriorityClass      string
}

func buildSlurmControllerFrom(clusterName string, maintenance *consts.MaintenanceMode, controller *slurmv1.SlurmNodeController) SlurmController {
	// Controller always has 1 replica
	statefulSet := buildStatefulSetWithMaxUnavailableFrom(
		naming.BuildStatefulSetName(consts.ComponentTypeController),
		consts.SingleReplicas,
		nil,
	)

	res := SlurmController{
		K8sNodeFilterName:    controller.K8sNodeFilterName,
		CustomInitContainers: controller.CustomInitContainers,
		HostUsers:            controller.HostUsers,
		ContainerSlurmctld: buildContainerFrom(
			controller.Slurmctld,
			consts.ContainerNameSlurmctld,
		),
		ContainerMunge: buildContainerFrom(
			controller.Munge,
			consts.ContainerNameMunge,
		),
		Service:            buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeController, clusterName)),
		StatefulSet:        statefulSet,
		VolumeSpool:        *controller.Volumes.Spool.DeepCopy(),
		VolumeJail:         *controller.Volumes.Jail.DeepCopy(),
		CustomVolumeMounts: controller.Volumes.CustomMounts,
		Maintenance:        maintenance,
		PriorityClass:      controller.PriorityClass,
	}

	for _, customVolumeMount := range controller.Volumes.CustomMounts {
		customMount := *customVolumeMount.DeepCopy()
		res.CustomVolumeMounts = append(res.CustomVolumeMounts, customMount)
	}

	return res
}
