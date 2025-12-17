package accounting_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/accounting"
)

func Test_BasePodTemplateSpec(t *testing.T) {
	expected := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: matchLabels,
		},
		Spec: corev1.PodSpec{
			NodeSelector: defaultNodeFilter[0].NodeSelector,
			Affinity:     defaultNodeFilter[0].Affinity,
			InitContainers: []corev1.Container{
				{
					Name:  consts.ContainerNameMunge,
					Image: imageMunge,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse(memory),
							corev1.ResourceCPU:    resource.MustParse(cpu),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse(memory),
						},
					},
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: port,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  consts.ContainerNameAccounting,
					Image: image,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse(memory),
							corev1.ResourceCPU:    resource.MustParse(cpu),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse(memory),
						},
					},
				},
			},
		},
	}

	result, err := accounting.BasePodTemplateSpec(
		defaultNameCluster, acc, defaultNodeFilter, defaultVolumeSources, matchLabels,
	)
	assert.NoError(t, err)

	assert.Equal(t, expected.Labels, result.Labels)

	// expected.Spec.InitContainers[1].Name == munge
	// expected.Spec.Containers[1].Name == accounting

	assert.Equal(t, expected.Spec.InitContainers[0].Name, result.Spec.InitContainers[1].Name)
	assert.Equal(t, expected.Spec.Containers[0].Name, result.Spec.Containers[0].Name)

	assert.Equal(t, expected.Spec.InitContainers[0].Image, result.Spec.InitContainers[1].Image)
	assert.Equal(t, expected.Spec.Containers[0].Image, result.Spec.Containers[0].Image)

	assert.Equal(t, expected.Spec.InitContainers[0].Resources, result.Spec.InitContainers[1].Resources)
	assert.Equal(t, expected.Spec.Containers[0].Resources, result.Spec.Containers[0].Resources)

	assert.Equal(t, expected.Spec.NodeSelector, result.Spec.NodeSelector)
	assert.Equal(t, expected.Spec.Affinity, result.Spec.Affinity)
}

func Test_BasePodTemplateSpec_PriorityClass(t *testing.T) {
	tests := []struct {
		name          string
		priorityClass string
		expectedClass string
	}{
		{
			name:          "empty priority class",
			priorityClass: "",
			expectedClass: "",
		},
		{
			name:          "custom priority class",
			priorityClass: "high-priority",
			expectedClass: "high-priority",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of acc and set PriorityClass
			testAcc := *acc
			testAcc.SlurmNode.PriorityClass = tt.priorityClass

			result, err := accounting.BasePodTemplateSpec(
				defaultNameCluster, &testAcc, defaultNodeFilter, defaultVolumeSources, matchLabels,
			)
			assert.NoError(t, err)

			// Check PriorityClassName
			assert.Equal(t, tt.expectedClass, result.Spec.PriorityClassName)
		})
	}
}
