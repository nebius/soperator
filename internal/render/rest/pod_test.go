package rest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_BasePodTemplateSpec_CustomLabelsAndAnnotations(t *testing.T) {
	valuesREST := &values.SlurmREST{
		SlurmNode: slurmv1.SlurmNode{
			K8sNodeFilterName: "test-filter",
		},
		Labels:      map[string]string{"gcore.com/project-id": "123"},
		Annotations: map[string]string{"gcore.com/note": "abc"},
		ContainerREST: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image: "test-rest-image",
			},
		},
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: &[]string{"test-volume-source"}[0],
		},
	}

	nodeFilters := []slurmv1.K8sNodeFilter{{Name: "test-filter"}}
	volumeSources := []slurmv1.VolumeSource{
		{
			Name:         "test-volume-source",
			VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{}},
		},
	}
	matchLabels := map[string]string{"key": "value"}

	result, err := BasePodTemplateSpec(
		"test-cluster", valuesREST, nodeFilters, volumeSources, matchLabels,
	)
	assert.NoError(t, err)

	assert.Equal(t, "123", result.Labels["gcore.com/project-id"])
	assert.Equal(t, "value", result.Labels["key"]) // matchLabels preserved
	assert.Equal(t, "abc", result.Annotations["gcore.com/note"])
}
