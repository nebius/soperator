package values

import (
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

type PopulateJail struct {
	slurmv1.PopulateJail

	Name string

	ContainerPopulateJail Container

	VolumeJail slurmv1.NodeVolume
}

func buildSlurmPopulateJailFrom(clusterName string, populateJail *slurmv1.PopulateJail) PopulateJail {
	return PopulateJail{
		PopulateJail: *populateJail.DeepCopy(),
		Name:         naming.BuildPopulateJailJobName(clusterName),
		ContainerPopulateJail: Container{
			Name:          consts.ContainerNamePopulateJail,
			NodeContainer: slurmv1.NodeContainer{Image: populateJail.Image},
		},
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To(consts.VolumeNameJail),
		},
	}
}
