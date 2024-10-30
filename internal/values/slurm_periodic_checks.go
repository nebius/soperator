package values

import (
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

type SlurmNCCLBenchmark struct {
	slurmv1.NCCLBenchmark

	Name string

	ContainerNCCLBenchmark Container

	VolumeJail slurmv1.NodeVolume
}

func buildSlurmNCCLBenchmarkFrom(clusterName string, ncclBenchmark *slurmv1.NCCLBenchmark) SlurmNCCLBenchmark {
	return SlurmNCCLBenchmark{
		NCCLBenchmark: *ncclBenchmark.DeepCopy(),
		Name:          naming.BuildCronJobNCCLBenchmarkName(clusterName),
		ContainerNCCLBenchmark: Container{
			Name:          consts.ContainerNameNCCLBenchmark,
			NodeContainer: slurmv1.NodeContainer{Image: ncclBenchmark.Image, ImagePullPolicy: ncclBenchmark.ImagePullPolicy},
		},
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To(consts.VolumeNameJail),
		},
	}
}
