package exporter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderPodTemplateSpec(t *testing.T) {
	saName := "slurm-exporter-sa"
	clusterValues := &values.SlurmCluster{
		NamespacedName: types.NamespacedName{
			Name:      "test-cluster",
			Namespace: "default",
		},
		CRVersion: "v1.0.0",
		SlurmExporter: values.SlurmExporter{
			SlurmNode: slurmv1.SlurmNode{
				K8sNodeFilterName: "test-filter",
			},
			Container: slurmv1.NodeContainer{
				Image: "exporter-image:latest",
			},
			VolumeJail: slurmv1.NodeVolume{
				VolumeSourceName: ptr.To(consts.VolumeNameJail),
			},
			ServiceAccountName: saName,
		},
		NodeFilters: []slurmv1.K8sNodeFilter{
			{
				Name:         "test-filter",
				NodeSelector: map[string]string{"node": "exporter"},
				Tolerations:  []corev1.Toleration{{Key: "test", Value: "true"}},
				Affinity:     &corev1.Affinity{},
			},
		},
		NodeRest: values.SlurmREST{},
		VolumeSources: []slurmv1.VolumeSource{
			{
				Name: consts.VolumeNameJail,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		},
	}
	initContainers := []corev1.Container{}
	matchLabels := map[string]string{"app": "slurm-exporter"}

	expectedPodTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"app": "slurm-exporter"},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: saName,
			NodeSelector:       map[string]string{"node": "exporter"},
			Tolerations:        []corev1.Toleration{{Key: "test", Value: "true"}},
			InitContainers:     []corev1.Container{},
		},
	}

	result := renderPodTemplateSpec(
		clusterValues,
		initContainers,
		matchLabels,
	)

	assert.Equal(t, expectedPodTemplate.ObjectMeta.Labels["app"], result.ObjectMeta.Labels["app"])
	assert.Equal(t, expectedPodTemplate.Spec.ServiceAccountName, result.Spec.ServiceAccountName)
	assert.Equal(t, expectedPodTemplate.Spec.NodeSelector["node"], result.Spec.NodeSelector["node"])
	assert.Len(t, result.Spec.Tolerations, len(expectedPodTemplate.Spec.Tolerations))
	assert.Equal(t, expectedPodTemplate.Spec.Tolerations[0].Key, result.Spec.Tolerations[0].Key)
	assert.Len(t, result.Spec.Containers, 1)
	assert.Len(t, result.Spec.InitContainers, len(expectedPodTemplate.Spec.InitContainers))
}

func Test_renderPodTemplateSpec_PriorityClass(t *testing.T) {
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
			clusterValues := &values.SlurmCluster{
				SlurmExporter: values.SlurmExporter{
					SlurmNode: slurmv1.SlurmNode{
						K8sNodeFilterName: "test-filter",
						PriorityClass:     tt.priorityClass,
					},
					Container: slurmv1.NodeContainer{
						Image: "test-image",
					},
				},
				NodeFilters: []slurmv1.K8sNodeFilter{
					{
						Name: "test-filter",
					},
				},
			}

			initContainers := []corev1.Container{}
			matchLabels := map[string]string{"app": "test"}

			result := renderPodTemplateSpec(clusterValues, initContainers, matchLabels)

			// Check PriorityClassName
			assert.Equal(t, tt.expectedClass, result.Spec.PriorityClassName)
		})
	}
}
