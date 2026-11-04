package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
)

func TestRenderNodeMetadataEnv(t *testing.T) {
	env, err := renderNodeMetadataEnv(
		"CPUs=128\nBoards=1 SocketsPerBoard=2 CoresPerSocket=32 threadspercore=2 gReS=gpu:nvidia_h200:8 "+
			`Features="a b" Weight= RealMemory=1024 Feature`,
		true,
	)
	require.NoError(t, err)
	assert.Equal(t, []corev1.EnvVar{
		{Name: "SOPERATOR_NODE_GRES", Value: "gpu:nvidia_h200:8"},
		{Name: "SOPERATOR_NODE_CPUS", Value: "128"},
		{Name: "SOPERATOR_NODE_THREADSPERCORE", Value: "2"},
		{Name: "SOPERATOR_NODE_CORESPERSOCKET", Value: "32"},
		{Name: "SOPERATOR_NODE_SOCKETSPERBOARD", Value: "2"},
		{Name: consts.EnvNodePlatformTag, Value: "8xH200"},
		{Name: consts.EnvNodePlatformTags, Value: "8xH200,8xGPU"},
	}, env)

	for _, static := range []string{"", "CPUs=8", "GRES=gpu:h200:8", "Gres=bad:::", `Features="a b"`} {
		t.Run("CPU "+static, func(t *testing.T) {
			env, err := renderNodeMetadataEnv(static, false)
			require.NoError(t, err)
			assert.Contains(t, env, corev1.EnvVar{Name: consts.EnvNodePlatformTag, Value: "CPU"})
			assert.Contains(t, env, corev1.EnvVar{Name: consts.EnvNodePlatformTags, Value: "CPU"})
		})
	}

	env, err = renderNodeMetadataEnv(`Gres="gpu:h100:8"`, true)
	require.NoError(t, err)
	assert.Contains(t, env, corev1.EnvVar{Name: "SOPERATOR_NODE_GRES", Value: "gpu:h100:8"})
	assert.Contains(t, env, corev1.EnvVar{Name: consts.EnvNodePlatformTags, Value: "8xH100,8xGPU"})

	_, err = renderNodeMetadataEnv("CPUs=128", true)
	require.ErrorContains(t, err, "provide Gres=gpu")
}

func TestGPUPlatformTags(t *testing.T) {
	for _, tc := range []struct {
		gres string
		tags []string
	}{
		{"gpu:nvidia_h100_80gb_hbm3:8", []string{"8xH100", "8xGPU"}},
		{"gpu:nvidia-h200:8", []string{"8xH200", "8xGPU"}},
		{"GPU:B200:8", []string{"8xB200", "8xGPU"}},
		{"gpu:b300:8", []string{"8xB300", "8xGPU"}},
		{"gpu:nvidia_gb300:4", []string{"4xGB300", "4xGPU"}},
		{"gpu:nvidia-a100:4", []string{"4xA100", "4xGPU"}},
		{"gpu:l40s:1", []string{"1xL40S", "1xGPU"}},
		{"gpu:8", []string{"8xGPU"}},
		{"gpu", []string{"1xGPU"}},
		{"gpu:nvidia:8", []string{"8xGPU"}},
		{"gpu:no_consume:8", []string{"8xGPU"}},
		{"gpu:h100:NO_CONSUME:8", []string{"8xH100", "8xGPU"}},
		{"gpu:h100:8(S:0-1)", []string{"8xH100", "8xGPU"}},
		{"gpu:h100:8(S:0,1),nic:2", []string{"8xH100", "8xGPU"}},
		{"gpu:1k", []string{"1024xGPU"}},
		{"mps:100,gpu:h200:4,gpu:nvidia_h200:4,bandwidth:lustre:no_consume:4G", []string{"8xH200", "8xGPU"}},
		{" gpu:h200:8 , shard:16,", []string{"8xH200", "8xGPU"}},
		{"gpu:h100:4,gpu:h200:4", []string{"8xGPU"}},
		{"gpu:h100:4,gpu:4", []string{"8xGPU"}},
		{"gpu:h100:2,gpu:h200:2,gpu:h100:4", []string{"8xGPU"}},
		{"gpus:h100:8,gpu:2", []string{"2xGPU"}},
	} {
		t.Run(tc.gres, func(t *testing.T) {
			tags, err := gpuPlatformTags(tc.gres)
			require.NoError(t, err)
			assert.Equal(t, tc.tags, tags)
		})
	}

	for _, gres := range []string{
		"", "mps:100", "gpu:0", "gpu:h200:0", "gpu:h200:-1", "gpu::8", "gpu:h200", "gpu:h200:8x",
		"gpu:h200:8:extra", "gpu:h200:18446744073709551616", "gpu:16777216P",
		"gpu:h200:18446744073709551615,gpu:h200:1",
	} {
		t.Run("invalid "+gres, func(t *testing.T) {
			_, err := gpuPlatformTags(gres)
			require.Error(t, err)
		})
	}
}
