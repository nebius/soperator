package benchmark

import (
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// renderContainerNCCLBenchmark renders [corev1.Container] for slurmctld
func renderContainerNCCLBenchmark(ncclBenchmark *values.SlurmNCCLBenchmark) corev1.Container {
	return corev1.Container{
		Name:            consts.ContainerNameNCCLBenchmark,
		Image:           ncclBenchmark.Image,
		ImagePullPolicy: corev1.PullAlways, // TODO use digest and set to corev1.PullIfNotPresent
		Env: []corev1.EnvVar{
			{
				Name:  "NCCL_MIN_BYTES",
				Value: ncclBenchmark.NcclSettings.MinBytes,
			},
			{
				Name:  "NCCL_MAX_BYTES",
				Value: ncclBenchmark.NcclSettings.MaxBytes,
			},
			{
				Name:  "NCCL_STEP_FACTOR",
				Value: ncclBenchmark.NcclSettings.StepFactor,
			},
			{
				Name:  "NCCL_BENCH_TIMOUT",
				Value: ncclBenchmark.NcclSettings.Timeout,
			},
			{
				Name:  "NCCL_THRESHOLD_MORE_THAN",
				Value: ncclBenchmark.NcclSettings.ThresholdMoreThan,
			},
			{
				Name:  "DRAIN_SLURM_STATE",
				Value: ncclBenchmark.FailureActions.SetSlurmNodeDrainState,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					consts.ContainerSecurityContextCapabilitySysAdmin,
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			common.RenderVolumeMountSlurmConfigs(),
			common.RenderVolumeMountJail(),
			common.RenderVolumeMountMungeKey(),
		},
	}
}
