package benchmark

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"

	"github.com/stretchr/testify/assert"
)

func Test_RenderContainerNCCLBenchmark(t *testing.T) {

	var namespace = "test-namespace"

	ncclBenchmark := &values.SlurmNCCLBenchmark{
		Name: "test-nccl-benchmark",

		ContainerNCCLBenchmark: values.Container{
			Name: consts.ContainerNameNCCLBenchmark,
		},
	}

	ncclBenchmark.ContainerNCCLBenchmark.Image = "test-image"
	ncclBenchmark.ContainerNCCLBenchmark.ImagePullPolicy = "IfNotPresent"
	ncclBenchmark.NCCLArguments.MinBytes = "1024"
	ncclBenchmark.NCCLArguments.MaxBytes = "2048"
	ncclBenchmark.NCCLArguments.StepFactor = "2"
	ncclBenchmark.NCCLArguments.Timeout = "300"
	ncclBenchmark.NCCLArguments.ThresholdMoreThan = "100"
	ncclBenchmark.NCCLArguments.UseInfiniband = true
	ncclBenchmark.FailureActions.SetSlurmNodeDrainState = true
	ncclBenchmark.Image = "test-image"

	container := renderContainerNCCLBenchmark(ncclBenchmark, namespace)

	assert.Equal(t, consts.ContainerNameNCCLBenchmark, container.Name)
	assert.Equal(t, "test-image", container.Image)
	assert.Equal(t, corev1.PullIfNotPresent, container.ImagePullPolicy)
	assert.Equal(t, "1024", getEnvVarValue(container, "NCCL_MIN_BYTES"))
	assert.Equal(t, "2048", getEnvVarValue(container, "NCCL_MAX_BYTES"))
	assert.Equal(t, "2", getEnvVarValue(container, "NCCL_STEP_FACTOR"))
	assert.Equal(t, "300", getEnvVarValue(container, "NCCL_BENCH_TIMOUT"))
	assert.Equal(t, "100", getEnvVarValue(container, "THRESHOLD_MORE_THAN"))
	assert.Equal(t, "true", getEnvVarValue(container, "DRAIN_SLURM_STATE"))
	assert.Equal(t, "true", getEnvVarValue(container, "USE_INFINIBAND"))
	assert.Equal(t, "true", getEnvVarValue(container, "SEND_JOBS_EVENTS"))
	assert.Equal(t, namespace, getEnvVarValue(container, "K8S_NAMESPACE"))
	assert.Equal(t, "true", getEnvVarValue(container, "SEND_OTEL_METRICS_GRPC"))
	assert.Equal(t, "false", getEnvVarValue(container, "SEND_OTEL_METRICS_HTTP"))
	assert.Len(t, container.VolumeMounts, 2)
}

func getEnvVarValue(container corev1.Container, name string) string {
	for _, envVar := range container.Env {
		if envVar.Name == name {
			return envVar.Value
		}
	}

	return ""
}
