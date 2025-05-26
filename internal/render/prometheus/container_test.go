package prometheus_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	. "nebius.ai/slurm-operator/internal/render/prometheus"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderContainerExporter(t *testing.T) {
	imageExporter := "test-image:latest"
	memoryExporter := "512Mi"
	cpuExporter := "500m"
	resourceExporter := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse(cpuExporter),
		corev1.ResourceMemory: resource.MustParse(memoryExporter),
	}

	containerParams := &values.SlurmExporter{
		ExporterContainer: slurmv1.ExporterContainer{
			NodeContainer: slurmv1.NodeContainer{
				Image:     imageExporter,
				Resources: resourceExporter,
			},
		},
	}

	expected := corev1.Container{
		Name:  consts.ContainerNameExporter,
		Image: imageExporter,
		Resources: corev1.ResourceRequirements{
			Requests: resourceExporter,
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memoryExporter),
			},
		},
	}

	result := RenderContainerExporter(containerParams)

	if _, ok := result.Resources.Limits[corev1.ResourceCPU]; ok {
		t.Errorf("ResourceCPU should not be set")
	}
	assert.Equal(t, expected.Name, result.Name)
	assert.Equal(t, expected.Image, result.Image)
	assert.Equal(t, expected.Resources, result.Resources)
}
