package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderVolumesForNodeSetMemoryBackedLimits(t *testing.T) {
	memory := resource.MustParse("96Gi")
	nodeSet := &values.SlurmNodeSet{
		ContainerSlurmd: values.Container{NodeContainer: slurmv1.NodeContainer{
			Resources: corev1.ResourceList{corev1.ResourceMemory: memory},
		}},
		GPU: &slurmv1alpha1.GPUSpec{},
	}

	volumes, _, err := renderVolumesAndClaimTemplateSpecsForNodeSet(nodeSet, &slurmv1.Secrets{})
	require.NoError(t, err)

	byName := make(map[string]corev1.Volume, len(volumes))
	for _, volume := range volumes {
		byName[volume.Name] = volume
	}
	for _, name := range []string{consts.VolumeNameInMemorySubmount, consts.VolumeNameSharedMemory} {
		emptyDir := byName[name].EmptyDir
		require.NotNil(t, emptyDir, name)
		assert.Equal(t, corev1.StorageMediumMemory, emptyDir.Medium, name)
		require.NotNil(t, emptyDir.SizeLimit, name)
		assert.True(t, memory.Equal(*emptyDir.SizeLimit), name)
	}
}
