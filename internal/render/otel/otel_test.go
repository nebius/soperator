package otel_test

import (
	"fmt"
	"testing"

	otelv1beta1 "github.com/open-telemetry/opentelemetry-operator/apis/v1beta1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	otel "nebius.ai/slurm-operator/internal/render/otel"
)

func Test_RenderOtelCollector_Image_True(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultImageName := "test-image"
	defaultNameCluster := "test-cluster"
	defaultPodTemplateName := "test-pod-template"

	telemetry := &slurmv1.Telemetry{
		OpenTelemetryCollector: &slurmv1.MetricsOpenTelemetryCollector{
			Enabled:               true,
			ReplicasOtelCollector: 1,
		},
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

	result, _ := otel.RenderOtelCollector(defaultNameCluster, defaultNamespace, telemetry, true, foundPodTemplate)

	assert.Equal(t, defaultImageName, result.Spec.OpenTelemetryCommonFields.Image)
}

func Test_RenderOtelCollector_Image_Default(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	telemetry := &slurmv1.Telemetry{
		OpenTelemetryCollector: &slurmv1.MetricsOpenTelemetryCollector{
			Enabled:               true,
			ReplicasOtelCollector: 1,
		},
	}

	var foundPodTemplate *corev1.PodTemplate

	result, _ := otel.RenderOtelCollector(defaultNameCluster, defaultNamespace, telemetry, true, foundPodTemplate)

	assert.Equal(t, otel.DefaultOtelCollectorImage, result.Spec.OpenTelemetryCommonFields.Image)
}

func Test_RenderOtelCollector_NodeSelector(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	telemetry := &slurmv1.Telemetry{
		OpenTelemetryCollector: &slurmv1.MetricsOpenTelemetryCollector{
			Enabled:               true,
			ReplicasOtelCollector: 1,
		},
	}

	// Test when NodeSelector is nil
	foundPodTemplate := &corev1.PodTemplate{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				NodeSelector: nil,
			},
		},
	}
	result, _ := otel.RenderOtelCollector(defaultNameCluster, defaultNamespace, telemetry, true, foundPodTemplate)
	assert.Nil(t, result.Spec.OpenTelemetryCommonFields.NodeSelector)

	// Test when NodeSelector is not empty
	nodeSelector := map[string]string{"disktype": "ssd"}
	foundPodTemplate.Template.Spec.NodeSelector = nodeSelector
	result, _ = otel.RenderOtelCollector(defaultNameCluster, defaultNamespace, telemetry, true, foundPodTemplate)
	assert.Equal(t, nodeSelector, result.Spec.OpenTelemetryCommonFields.NodeSelector)
}

func Test_RenderOtelCollector_Resources(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	telemetry := &slurmv1.Telemetry{
		OpenTelemetryCollector: &slurmv1.MetricsOpenTelemetryCollector{
			Enabled:               true,
			ReplicasOtelCollector: 1,
		},
	}

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

	result, _ := otel.RenderOtelCollector(defaultNameCluster, defaultNamespace, telemetry, true, foundPodTemplate)

	assert.Equal(t, resource.MustParse("1Gi"), result.Spec.OpenTelemetryCommonFields.Resources.Limits[corev1.ResourceMemory])
	assert.Equal(t, resource.MustParse("1"), result.Spec.OpenTelemetryCommonFields.Resources.Requests[corev1.ResourceCPU])
}

func Test_RenderOtelCollector_Env(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"
	defaultPodTemplateName := "test-pod-template"

	telemetry := &slurmv1.Telemetry{
		OpenTelemetryCollector: &slurmv1.MetricsOpenTelemetryCollector{
			Enabled:               true,
			ReplicasOtelCollector: 1,
			PodTemplateNameRef:    &defaultPodTemplateName,
		},
	}

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

	result, _ := otel.RenderOtelCollector(defaultNameCluster, defaultNamespace, telemetry, true, foundPodTemplate)

	assert.Equal(t, "TEST_ENV", result.Spec.OpenTelemetryCommonFields.Env[0].Name)
	assert.Equal(t, "test", result.Spec.OpenTelemetryCommonFields.Env[0].Value)
}

func Test_RenderOtelCollector_Endpoint_Default(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"

	telemetry := &slurmv1.Telemetry{
		OpenTelemetryCollector: &slurmv1.MetricsOpenTelemetryCollector{
			Enabled:               true,
			ReplicasOtelCollector: 1,
		},
	}

	var foundPodTemplate *corev1.PodTemplate
	anyConfig := otelv1beta1.AnyConfig{
		Object: map[string]interface{}{
			"otlp": map[string]interface{}{
				"protocols": map[string]interface{}{
					"grpc": map[string]interface{}{
						"endpoint": "0.0.0.0:4317",
					},
				},
			},
		},
	}

	result, _ := otel.RenderOtelCollector(defaultNameCluster, defaultNamespace, telemetry, true, foundPodTemplate)
	assert.Equal(t, anyConfig.Object["otlp"], result.Spec.Config.Receivers.Object["otlp"])
}

func Test_RenderOtelCollector_Endpoint_Custom_Port(t *testing.T) {
	defaultNamespace := "test-namespace"
	defaultNameCluster := "test-cluster"
	defaultOtelCollectorPort := int32(4317)
	customOtelCollectorPort := int32(4318)

	var foundPodTemplate *corev1.PodTemplate

	defaultMetricsPort := &slurmv1.Telemetry{
		OpenTelemetryCollector: &slurmv1.MetricsOpenTelemetryCollector{
			Enabled:               true,
			ReplicasOtelCollector: 1,
			OtelCollectorPort:     defaultOtelCollectorPort,
		},
	}

	defaultAnyConfigPort := otelv1beta1.AnyConfig{
		Object: map[string]interface{}{
			"otlp": map[string]interface{}{
				"protocols": map[string]interface{}{
					"grpc": map[string]interface{}{
						"endpoint": fmt.Sprintf("0.0.0.0:%d", defaultOtelCollectorPort),
					},
				},
			},
		},
	}

	customMetricsPort := &slurmv1.Telemetry{
		OpenTelemetryCollector: &slurmv1.MetricsOpenTelemetryCollector{
			Enabled:               true,
			ReplicasOtelCollector: 1,
			OtelCollectorPort:     customOtelCollectorPort,
		},
	}

	customAnyConfigPort := otelv1beta1.AnyConfig{
		Object: map[string]interface{}{
			"otlp": map[string]interface{}{
				"protocols": map[string]interface{}{
					"grpc": map[string]interface{}{
						"endpoint": fmt.Sprintf("0.0.0.0:%d", customOtelCollectorPort),
					},
				},
			},
		},
	}

	result, _ := otel.RenderOtelCollector(defaultNameCluster, defaultNamespace, defaultMetricsPort, true, foundPodTemplate)
	assert.Equal(t, defaultAnyConfigPort.Object["otlp"], result.Spec.Config.Receivers.Object["otlp"])
	result, _ = otel.RenderOtelCollector(defaultNameCluster, defaultNamespace, customMetricsPort, true, foundPodTemplate)
	assert.Equal(t, customAnyConfigPort.Object["otlp"], result.Spec.Config.Receivers.Object["otlp"])
}
