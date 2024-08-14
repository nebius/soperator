package otel

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"

	otelv1beta1 "github.com/open-telemetry/opentelemetry-operator/apis/v1beta1"
)

const (
	DefaultOtelCollectorImage = "otel/opentelemetry-collector:0.106.1"
)

func RenderOtelCollector(clusterName,
	namespace string,
	metrics *slurmv1.Telemetry,
	foundPodTemplate *corev1.PodTemplate,
) (otelv1beta1.OpenTelemetryCollector, error) {
	if metrics == nil || metrics.OpenTelemetryCollector == nil || !metrics.OpenTelemetryCollector.EnabledOtelCollector {
		return otelv1beta1.OpenTelemetryCollector{}, errors.New("OpenTelemetry Collector is not enabled")
	}

	replicasOtelCollector := metrics.OpenTelemetryCollector.ReplicasOtelCollector
	imageOtelCollector := renderPodTemplateImage(foundPodTemplate)

	enableMetrics := false
	if metrics.Prometheus == nil {
		enableMetrics = metrics.Prometheus.Enabled
	}

	var securityContext *corev1.PodSecurityContext
	var nodeSelector map[string]string
	var tolerations []corev1.Toleration
	var affinity *corev1.Affinity
	var resources corev1.ResourceRequirements
	var topologySpreadConstraints []corev1.TopologySpreadConstraint
	var initContainers []corev1.Container
	var volumes []corev1.Volume
	var hostNetwork bool
	var lifecycle *corev1.Lifecycle
	var env []corev1.EnvVar
	var envFrom []corev1.EnvFromSource
	var podAnnotations map[string]string
	var imagePullPolicy corev1.PullPolicy = corev1.PullIfNotPresent
	var otelCollectorPort int32 = 4317

	if metrics.OpenTelemetryCollector != nil && metrics.OpenTelemetryCollector.OtelCollectorPort != 0 {
		otelCollectorPort = metrics.OpenTelemetryCollector.OtelCollectorPort
	}

	if foundPodTemplate != nil {
		securityContext = foundPodTemplate.Template.Spec.SecurityContext
		nodeSelector = foundPodTemplate.Template.Spec.NodeSelector
		tolerations = foundPodTemplate.Template.Spec.Tolerations
		affinity = foundPodTemplate.Template.Spec.Affinity
		if len(foundPodTemplate.Template.Spec.Containers) > 0 {
			imagePullPolicy = foundPodTemplate.Template.Spec.Containers[0].ImagePullPolicy
			resources = foundPodTemplate.Template.Spec.Containers[0].Resources
			lifecycle = foundPodTemplate.Template.Spec.Containers[0].Lifecycle
			env = foundPodTemplate.Template.Spec.Containers[0].Env
			envFrom = foundPodTemplate.Template.Spec.Containers[0].EnvFrom
		}
		topologySpreadConstraints = foundPodTemplate.Template.Spec.TopologySpreadConstraints
		initContainers = foundPodTemplate.Template.Spec.InitContainers
		volumes = foundPodTemplate.Template.Spec.Volumes
		hostNetwork = foundPodTemplate.Template.Spec.HostNetwork
		podAnnotations = foundPodTemplate.Template.Annotations
	}

	return otelv1beta1.OpenTelemetryCollector{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "opentelemetry.io/v1beta1",
			Kind:       "OpenTelemetryCollector",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: otelv1beta1.OpenTelemetryCollectorSpec{
			Config: otelv1beta1.Config{
				Exporters: otelv1beta1.AnyConfig{
					Object: map[string]interface{}{
						"prometheus": map[string]interface{}{
							"endpoint":                         "0.0.0.0:9102",
							"send_timestamps":                  true,
							"metric_expiration":                "180m", // 3h CronJob NCClBenchmark interval
							"enable_open_metrics":              true,
							"add_metric_suffixes":              false,
							"resource_to_telemetry_conversion": map[string]interface{}{"enabled": true},
						},
					},
				},
				Extensions: &otelv1beta1.AnyConfig{
					Object: map[string]interface{}{
						"health_check": map[string]interface{}{
							"endpoint": "0.0.0.0:13133",
						},
					},
				},
				Receivers: otelv1beta1.AnyConfig{
					Object: map[string]interface{}{
						"otlp": map[string]interface{}{
							"protocols": map[string]interface{}{
								"grpc": map[string]interface{}{
									"endpoint": fmt.Sprintf("0.0.0.0:%d", otelCollectorPort),
								},
							},
						},
					},
				},
				Service: otelv1beta1.Service{
					Extensions: &[]string{"health_check"},
					Pipelines: map[string]*otelv1beta1.Pipeline{
						"metrics": {
							Exporters:  []string{"prometheus"},
							Processors: []string{"memory_limiter", "batch"},
							Receivers:  []string{"otlp"},
						},
					},
				},
				Processors: &otelv1beta1.AnyConfig{
					Object: map[string]interface{}{
						"batch": map[string]interface{}{
							"send_batch_max_size": 700,
							"send_batch_size":     250,
						},
						"memory_limiter": map[string]interface{}{
							"check_interval":         "5s",
							"limit_percentage":       80,
							"spike_limit_percentage": 25,
						},
					},
				},
			},
			Mode: otelv1beta1.ModeDeployment,

			Observability: otelv1beta1.ObservabilitySpec{
				Metrics: otelv1beta1.MetricsConfigSpec{
					EnableMetrics: enableMetrics,
				},
			},
			OpenTelemetryCommonFields: otelv1beta1.OpenTelemetryCommonFields{
				Image:                     imageOtelCollector,
				ImagePullPolicy:           imagePullPolicy,
				Replicas:                  &replicasOtelCollector,
				ManagementState:           otelv1beta1.ManagementStateManaged,
				PodSecurityContext:        securityContext,
				NodeSelector:              nodeSelector,
				Tolerations:               tolerations,
				Affinity:                  affinity,
				Resources:                 resources,
				TopologySpreadConstraints: topologySpreadConstraints,
				InitContainers:            initContainers,
				Volumes:                   volumes,
				HostNetwork:               hostNetwork,
				Lifecycle:                 lifecycle,
				Env:                       env,
				EnvFrom:                   envFrom,
				PodAnnotations:            podAnnotations,
			},
		},
	}, nil
}

func renderPodTemplateImage(podTemplate *corev1.PodTemplate) string {
	if podTemplate != nil && len(podTemplate.Template.Spec.Containers) > 0 {
		return podTemplate.Template.Spec.Containers[0].Image
	}
	return DefaultOtelCollectorImage
}
