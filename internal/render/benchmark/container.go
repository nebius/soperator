package benchmark

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

var (
	OtelCollectorPort int32 = 4317
	OtelCollectorPath       = "/v1/metrics"
)

// renderContainerNCCLBenchmark renders [corev1.Container] for slurmctld
func renderContainerNCCLBenchmark(
	ncclBenchmark *values.SlurmNCCLBenchmark, namespace string) corev1.Container {

	var sendJobsEvents bool
	var sendOtelMetricsGrpc bool
	var sendOtelMetricsHttp bool

	otelCollectorHost := "localhost"

	otelCollectorEndpoint := fmt.Sprintf("%s:%d", otelCollectorHost, OtelCollectorPort)

	return corev1.Container{
		Name:            ncclBenchmark.ContainerNCCLBenchmark.Name,
		Image:           ncclBenchmark.Image,
		ImagePullPolicy: ncclBenchmark.ContainerNCCLBenchmark.ImagePullPolicy,
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
				Name:  "K8S_NAMESPACE",
				Value: namespace,
			},
			{
				Name:  "SEND_JOBS_EVENTS",
				Value: strconv.FormatBool(sendJobsEvents),
			},
			{
				Name:  "SEND_OTEL_METRICS_GRPC",
				Value: strconv.FormatBool(sendOtelMetricsGrpc),
			},
			{
				Name:  "SEND_OTEL_METRICS_HTTP",
				Value: strconv.FormatBool(sendOtelMetricsHttp),
			},
			{
				Name:  "OTEL_COLLECTOR_ENDPOINT",
				Value: otelCollectorEndpoint,
			},
			{
				Name:  "OTEL_COLLECTOR_PATH",
				Value: OtelCollectorPath,
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
			common.RenderVolumeMountJail(),
			common.RenderVolumeMountMungeKey(),
		},
	}
}
