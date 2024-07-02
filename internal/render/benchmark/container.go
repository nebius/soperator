package benchmark

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// renderContainerNCCLBenchmark renders [corev1.Container] for slurmctld
func renderContainerNCCLBenchmark(ncclBenchmark *values.SlurmNCCLBenchmark) corev1.Container {
	return corev1.Container{
		Name:            ncclBenchmark.ContainerNCCLBenchmark.Name,
		Image:           ncclBenchmark.Image,
		ImagePullPolicy: corev1.PullAlways, // TODO use digest and set to corev1.PullIfNotPresent
		Env: []corev1.EnvVar{
			{
				Name:  "NCCL_MIN_BYTES",
				Value: ncclBenchmark.NCCLSettings.MinBytes,
			},
			{
				Name:  "NCCL_MAX_BYTES",
				Value: ncclBenchmark.NCCLSettings.MaxBytes,
			},
			{
				Name:  "NCCL_STEP_FACTOR",
				Value: ncclBenchmark.NCCLSettings.StepFactor,
			},
			{
				Name:  "NCCL_BENCH_TIMOUT",
				Value: ncclBenchmark.NCCLSettings.Timeout,
			},
			{
				Name:  "THRESHOLD_MORE_THAN",
				Value: ncclBenchmark.NCCLSettings.ThresholdMoreThan,
			},
			{
				Name:  "DRAIN_SLURM_STATE",
				Value: strconv.FormatBool(ncclBenchmark.FailureActions.SetSlurmNodeDrainState),
			},
			{
				Name:  "USE_INFINIBAND",
				Value: strconv.FormatBool(ncclBenchmark.NCCLSettings.UseInfiniband),
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
