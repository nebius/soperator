package benchmark

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// renderContainerNCCLBenchmark renders [corev1.Container] for slurmctld
func renderContainerNCCLBenchmark(
	ncclBenchmark *values.SlurmNCCLBenchmark,
	metrics *slurmv1.Metrics,
	clusterName string) corev1.Container {
	var sendJobsEvents bool
	if ncclBenchmark.SendJobsEvents != nil && *ncclBenchmark.SendJobsEvents {
		sendJobsEvents = true
	}

	var sendOtelMetrics bool
	if ncclBenchmark.SendOTELMetrics != nil && *ncclBenchmark.SendOTELMetrics {
		sendOtelMetrics = true
	}

	otelCollectorEndpoint := "localhost:4317"
	if metrics != nil && metrics.EnableOtelCollector != nil {
		otelCollectorEndpoint = naming.BuildOtelSvc(clusterName) + ":4317"
	} else if ncclBenchmark.OtelCollectorEndpoint != nil {
		otelCollectorEndpoint = *ncclBenchmark.OtelCollectorEndpoint
	}

	return corev1.Container{
		Name:            ncclBenchmark.ContainerNCCLBenchmark.Name,
		Image:           ncclBenchmark.Image,
		ImagePullPolicy: corev1.PullAlways, // TODO use digest and set to corev1.PullIfNotPresent
		Env: []corev1.EnvVar{
			{
				Name:  "NCCL_MIN_BYTES",
				Value: ncclBenchmark.NCCLArguments.MinBytes,
			},
			{
				Name:  "NCCL_MAX_BYTES",
				Value: ncclBenchmark.NCCLArguments.MaxBytes,
			},
			{
				Name:  "NCCL_STEP_FACTOR",
				Value: ncclBenchmark.NCCLArguments.StepFactor,
			},
			{
				Name:  "NCCL_BENCH_TIMOUT",
				Value: ncclBenchmark.NCCLArguments.Timeout,
			},
			{
				Name:  "THRESHOLD_MORE_THAN",
				Value: ncclBenchmark.NCCLArguments.ThresholdMoreThan,
			},
			{
				Name:  "DRAIN_SLURM_STATE",
				Value: strconv.FormatBool(ncclBenchmark.FailureActions.SetSlurmNodeDrainState),
			},
			{
				Name:  "USE_INFINIBAND",
				Value: strconv.FormatBool(ncclBenchmark.NCCLArguments.UseInfiniband),
			},
			{
				Name:  "SEND_JOBS_EVENTS",
				Value: strconv.FormatBool(sendJobsEvents),
			},
			{
				Name:  "SEND_OTEL_METRICS",
				Value: strconv.FormatBool(sendOtelMetrics),
			},
			{
				Name:  "OTEL_COLLECTOR_ENDPOINT",
				Value: otelCollectorEndpoint,
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
