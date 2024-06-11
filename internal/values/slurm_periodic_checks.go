package values

import (
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

type SlurmNCCLBenchmark struct {
	slurmv1.NCCLBenchmark

	ContainerNCCLBenchmark Container

	VolumeJail slurmv1.NodeVolume
}

func buildSlurmNCCLBenchmarkFrom(ncclBenchmark *slurmv1.NCCLBenchmark) SlurmNCCLBenchmark {
	return SlurmNCCLBenchmark{
		NCCLBenchmark: *ncclBenchmark.DeepCopy(),
		ContainerNCCLBenchmark: Container{
			Name:          ncclBenchmark.Name,
			NodeContainer: slurmv1.NodeContainer{Image: ncclBenchmark.Image},
		},
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To(consts.VolumeNameJail),
		},
	}
}
