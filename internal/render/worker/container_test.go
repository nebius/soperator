package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderContainerCgroupMaker(t *testing.T) {
	container := &values.Container{
		NodeContainer: slurmv1.NodeContainer{
			Image: "test-image",
		},
		Name: "test-container",
	}

	result := renderContainerCgroupMaker(container)

	assert.Equal(t, consts.ContainerNameCgroupMaker, result.Name)
	assert.Equal(t, "test-image", result.Image)
}
