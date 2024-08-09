package otel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

func Test_RenderEnableMetrics(t *testing.T) {
	// Test when metrics is nil
	// assert.False(t, renderEnableMetrics(nil))

	// Test when metrics is not nil but EnableMetrics is nil
	metrics := &slurmv1.Metrics{}
	assert.False(t, renderEnableMetrics(metrics))

	// Test when EnableMetrics is set to true
	enableMetrics := true
	metrics.EnableMetrics = &enableMetrics
	assert.True(t, renderEnableMetrics(metrics))

	// Test when EnableMetrics is set to false
	enableMetrics = false
	metrics.EnableMetrics = &enableMetrics
	assert.False(t, renderEnableMetrics(metrics))
}

func Test_RenderReplicasOtelCollector(t *testing.T) {
	// Test when metrics is nil
	assert.Equal(t, int32(1), *renderReplicasOtelCollector(nil))

	// Test when metrics is not nil but ReplicasOtelCollector is nil
	metrics := &slurmv1.Metrics{}
	assert.Equal(t, int32(1), *renderReplicasOtelCollector(metrics))

	// Test when ReplicasOtelCollector is set to a value
	var replicas int32 = 3
	metrics.ReplicasOtelCollector = &replicas
	assert.Equal(t, replicas, *renderReplicasOtelCollector(metrics))
}

func Test_RenderPodTemplateImage(t *testing.T) {
	defaultImageName := "test-image"

	// Test when podTemplate is nil
	assert.Equal(t, DefaultOtelCollectorImage, renderPodTemplateImage(nil))

	// Test when podTemplate is not nil but Containers is empty
	podTemplate := &corev1.PodTemplate{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{},
			},
		},
	}
	assert.Equal(t, DefaultOtelCollectorImage, renderPodTemplateImage(podTemplate))

	// Test when Containers is not empty
	podTemplate.Template.Spec.Containers = append(podTemplate.Template.Spec.Containers, corev1.Container{
		Image: defaultImageName,
	})
	assert.Equal(t, defaultImageName, renderPodTemplateImage(podTemplate))
}

func Test_RenderOtelCollector_Image_True(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultImageName := "test-image"
	defaultNameCluster := "test-cluster"
	defaultPodTemplateName := "test-pod-template"

	metrics := &slurmv1.Metrics{
		EnableMetrics:         new(bool),
		ReplicasOtelCollector: new(int32),
		EnableOtelCollector:   new(bool),
		PodTemplateNameRef:    &defaultPodTemplateName,
	}
	*metrics.EnableOtelCollector = true

	foundPodTemplate := &corev1.PodTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultPodTemplateName,
			Namespace: defaultNamespace,
		},
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Image: defaultImageName,
					},
				},
			},
		},
	}

	result, _ := RenderOtelCollector(defaultNameCluster, defaultNamespace, metrics, foundPodTemplate)

	assert.Equal(t, defaultImageName, result.Spec.OpenTelemetryCommonFields.Image)
}

func Test_RenderOtelCollector_Image_False(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultImageName := "test-image"
	defaultNameCluster := "test-cluster"
	defaultPodTemplateName := "test-pod-template"

	metrics := &slurmv1.Metrics{
		EnableMetrics:         new(bool),
		ReplicasOtelCollector: new(int32),
		EnableOtelCollector:   new(bool),
		PodTemplateNameRef:    &defaultPodTemplateName,
	}

	foundPodTemplate := &corev1.PodTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultPodTemplateName,
			Namespace: defaultNamespace,
		},
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Image: defaultImageName,
					},
				},
			},
		},
	}

	result, _ := RenderOtelCollector(defaultNameCluster, defaultNamespace, metrics, foundPodTemplate)

	assert.Equal(t, "", result.Spec.OpenTelemetryCommonFields.Image)
}

func Test_RenderOtelCollector_NodeSelector(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	metrics := &slurmv1.Metrics{
		EnableMetrics:         new(bool),
		ReplicasOtelCollector: new(int32),
		EnableOtelCollector:   new(bool),
	}
	*metrics.EnableOtelCollector = true

	// Test when NodeSelector is nil
	foundPodTemplate := &corev1.PodTemplate{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				NodeSelector: nil,
			},
		},
	}
	result, _ := RenderOtelCollector(defaultNameCluster, defaultNamespace, metrics, foundPodTemplate)
	assert.Nil(t, result.Spec.OpenTelemetryCommonFields.NodeSelector)

	// Test when NodeSelector is not empty
	nodeSelector := map[string]string{"disktype": "ssd"}
	foundPodTemplate.Template.Spec.NodeSelector = nodeSelector
	result, _ = RenderOtelCollector(defaultNameCluster, defaultNamespace, metrics, foundPodTemplate)
	assert.Equal(t, nodeSelector, result.Spec.OpenTelemetryCommonFields.NodeSelector)
}

func Test_RenderOtelCollector_Resources(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	metrics := &slurmv1.Metrics{
		EnableMetrics:         new(bool),
		ReplicasOtelCollector: new(int32),
		EnableOtelCollector:   new(bool),
	}
	*metrics.EnableOtelCollector = true

	foundPodTemplate := &corev1.PodTemplate{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("1"),
							},
						},
					},
				},
			},
		},
	}

	result, _ := RenderOtelCollector(defaultNameCluster, defaultNamespace, metrics, foundPodTemplate)

	assert.Equal(t, resource.MustParse("1Gi"), result.Spec.OpenTelemetryCommonFields.Resources.Limits[corev1.ResourceMemory])
	assert.Equal(t, resource.MustParse("1"), result.Spec.OpenTelemetryCommonFields.Resources.Requests[corev1.ResourceCPU])
}

func Test_RenderOtelCollector_Env(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"
	defaultPodTemplateName := "test-pod-template"

	metrics := &slurmv1.Metrics{
		EnableMetrics:         new(bool),
		ReplicasOtelCollector: new(int32),
		EnableOtelCollector:   new(bool),
		PodTemplateNameRef:    &defaultPodTemplateName,
	}
	*metrics.EnableOtelCollector = true

	foundPodTemplate := &corev1.PodTemplate{
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultPodTemplateName,
				Namespace: defaultNamespace,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Env: []corev1.EnvVar{
							{
								Name:  "TEST_ENV",
								Value: "test",
							},
						},
					},
				},
			},
		},
	}

	result, _ := RenderOtelCollector(defaultNameCluster, defaultNamespace, metrics, foundPodTemplate)

	assert.Equal(t, "TEST_ENV", result.Spec.OpenTelemetryCommonFields.Env[0].Name)
	assert.Equal(t, "test", result.Spec.OpenTelemetryCommonFields.Env[0].Value)
}
