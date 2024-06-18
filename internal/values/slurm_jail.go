package values

import (
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

type PopulateJail struct {
	slurmv1.PopulateJail

	ContainerPopulateJail Container

	VolumeJail slurmv1.NodeVolume
}

func buildSlurmPopulateJailFrom(populateJail *slurmv1.PopulateJail) PopulateJail {
	return PopulateJail{
		PopulateJail: *populateJail.DeepCopy(),
		ContainerPopulateJail: Container{
			Name:          consts.ContainerNamePopulateJail,
			NodeContainer: slurmv1.NodeContainer{Image: populateJail.Image},
		},
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To(consts.VolumeNameJail),
		},
	}
}
