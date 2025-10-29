package values

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// SlurmWorker contains the data needed to deploy and reconcile the Slurm Workers
type SlurmWorker struct {
	slurmv1.SlurmNode

	ContainerSlurmd      Container
	ContainerMunge       Container
	CustomInitContainers []corev1.Container

	SupervisordConfigMapDefault bool
	SupervisordConfigMapName    string

	IsSSHDConfigMapDefault bool
	SSHDConfigMapName      string

	WorkerAnnotations map[string]string

	CgroupVersion  string
	EnableGDRCopy  bool
	SlurmNodeExtra string
	PriorityClass  string

	Service     Service
	StatefulSet StatefulSet

	VolumeSpool               slurmv1.NodeVolume
	VolumeJail                slurmv1.NodeVolume
	JailSubMounts             []slurmv1.NodeVolumeMount
	CustomVolumeMounts        []slurmv1.NodeVolumeMount
	SharedMemorySize          *resource.Quantity
	UseDefaultAppArmorProfile bool
	Maintenance               *consts.MaintenanceMode
}

func buildSlurmWorkerFrom(
	clusterName string,
	maintenance *consts.MaintenanceMode,
	worker *slurmv1.SlurmNodeWorker,
	useDefaultAppArmorProfile bool,
) SlurmWorker {
	supervisordConfigName := worker.SupervisordConfigMapRefName
	supervisordConfigDefault := supervisordConfigName == ""
	if supervisordConfigDefault {
		supervisordConfigName = naming.BuildConfigMapSupervisordName(clusterName)
	}

	sshdConfigMapName := worker.SSHDConfigMapRefName
	isSSHDConfigDefault := sshdConfigMapName == ""
	if isSSHDConfigDefault {
		sshdConfigMapName = naming.BuildConfigMapSSHDConfigsNameWorker(clusterName)
	}

	res := SlurmWorker{
		SlurmNode: *worker.SlurmNode.DeepCopy(),
		ContainerSlurmd: buildContainerFrom(
			worker.Slurmd,
			consts.ContainerNameSlurmd,
		),
		ContainerMunge: buildContainerFrom(
			worker.Munge,
			consts.ContainerNameMunge,
		),
		CustomInitContainers:        worker.CustomInitContainers,
		SupervisordConfigMapDefault: supervisordConfigDefault,
		SupervisordConfigMapName:    supervisordConfigName,
		WorkerAnnotations:           worker.WorkerAnnotations,
		Service:                     buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeWorker, clusterName)),
		StatefulSet: buildStatefulSetWithMaxUnavailableFrom(
			naming.BuildStatefulSetName(consts.ComponentTypeWorker),
			worker.SlurmNode.Size,
			worker.MaxUnavailable,
		),
		VolumeSpool:               *worker.Volumes.Spool.DeepCopy(),
		VolumeJail:                *worker.Volumes.Jail.DeepCopy(),
		SharedMemorySize:          worker.Volumes.SharedMemorySize,
		CgroupVersion:             worker.CgroupVersion,
		EnableGDRCopy:             worker.EnableGDRCopy,
		PriorityClass:             worker.PriorityClass,
		UseDefaultAppArmorProfile: useDefaultAppArmorProfile,
		SlurmNodeExtra:            worker.SlurmNodeExtra,
		SSHDConfigMapName:         sshdConfigMapName,
		IsSSHDConfigMapDefault:    isSSHDConfigDefault,
		Maintenance:               maintenance,
	}
	for _, jailSubMount := range worker.Volumes.JailSubMounts {
		subMount := *jailSubMount.DeepCopy()
		res.JailSubMounts = append(res.JailSubMounts, subMount)
	}
	for _, customVolumeMount := range worker.Volumes.CustomMounts {
		customMount := *customVolumeMount.DeepCopy()
		res.CustomVolumeMounts = append(res.CustomVolumeMounts, customMount)
	}

	return res
}
