package exporter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderPodTemplateSpec(t *testing.T) {
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
	}
	initContainers := []corev1.Container{}
	matchLabels := map[string]string{"app": "slurm-exporter"}

	expectedPodTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"app": "slurm-exporter"},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "test-cluster-exporter-sa",
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
