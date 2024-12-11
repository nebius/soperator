package values

import (
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// SlurmWorker contains the data needed to deploy and reconcile the Slurm Workers
type SlurmWorker struct {
	slurmv1.SlurmNode

	NCCLSettings slurmv1.NCCLSettings

	ContainerToolkitValidation Container
	ContainerSlurmd            Container
	ContainerMunge             Container

	CgroupVersion string

	Service     Service
	StatefulSet StatefulSet

	VolumeSpool      slurmv1.NodeVolume
	VolumeJail       slurmv1.NodeVolume
	JailSubMounts    []slurmv1.NodeVolumeJailSubMount
	SharedMemorySize *resource.Quantity
	SshdEnabled      bool
	ContainerSshd    Container
}

func buildSlurmWorkerFrom(
	clusterName string,
	worker *slurmv1.SlurmNodeWorker,
	ncclSettings *slurmv1.NCCLSettings,
) SlurmWorker {
	res := SlurmWorker{
		SlurmNode:    *worker.SlurmNode.DeepCopy(),
		NCCLSettings: *ncclSettings.DeepCopy(),
		ContainerToolkitValidation: Container{
			NodeContainer: slurmv1.NodeContainer{
				Image:           "nvcr.io/nvidia/cloud-native/gpu-operator-validator:v23.9.1",
				ImagePullPolicy: worker.Slurmd.ImagePullPolicy, // for now the same as Slurmd
			},
			Name: consts.ContainerNameToolkitValidation,
		},
		ContainerSlurmd: buildContainerFrom(
			worker.Slurmd,
			consts.ContainerNameSlurmd,
		),
		ContainerMunge: buildContainerFrom(
			worker.Munge,
			consts.ContainerNameMunge,
		),
		Service: buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeWorker, clusterName)),
		StatefulSet: buildStatefulSetFrom(
			naming.BuildStatefulSetName(consts.ComponentTypeWorker, clusterName),
			worker.SlurmNode.Size,
		),
		VolumeSpool:      *worker.Volumes.Spool.DeepCopy(),
		VolumeJail:       *worker.Volumes.Jail.DeepCopy(),
		SharedMemorySize: worker.Volumes.SharedMemorySize,
		CgroupVersion:    worker.CgroupVersion,
		SshdEnabled:      worker.SlurmNodeWorkerSshd.Enabled,
		ContainerSshd: buildContainerFrom(
			worker.SlurmNodeWorkerSshd.NodeContainer,
			consts.ContainerNameSshd,
		),
	}
	for _, jailSubMount := range worker.Volumes.JailSubMounts {
		subMount := *jailSubMount.DeepCopy()
		res.JailSubMounts = append(res.JailSubMounts, subMount)
	}

	return res
}
