package login

import (
	"fmt"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
)

func TestRenderHorizontalPodAutoscaler(t *testing.T) {
	hpa, err := RenderHorizontalPodAutoscaler("test-namespace", "login-test", 3, 6, 75)
	if err != nil {
		t.Fatalf("RenderHorizontalPodAutoscaler() error = %v", err)
	}
	if hpa.Spec.ScaleTargetRef.APIVersion != "apps.kruise.io/v1beta1" ||
		hpa.Spec.ScaleTargetRef.Kind != "StatefulSet" ||
		hpa.Spec.ScaleTargetRef.Name != "login-test" {
		t.Fatalf("unexpected scale target: %+v", hpa.Spec.ScaleTargetRef)
	}
	if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas != 3 || hpa.Spec.MaxReplicas != 6 {
		t.Fatalf("unexpected replica bounds: min=%v max=%d", hpa.Spec.MinReplicas, hpa.Spec.MaxReplicas)
	}
	if len(hpa.Spec.Metrics) != 1 {
		t.Fatalf("metrics count = %d, want 1", len(hpa.Spec.Metrics))
	}
	metric := hpa.Spec.Metrics[0]
	if metric.Type != autoscalingv2.ContainerResourceMetricSourceType || metric.ContainerResource == nil {
		t.Fatalf("unexpected metric: %+v", metric)
	}
	if metric.ContainerResource.Container != consts.ContainerNameSshd || metric.ContainerResource.Name != corev1.ResourceCPU {
		t.Fatalf("unexpected container resource metric: %+v", metric.ContainerResource)
	}
	if metric.ContainerResource.Target.AverageUtilization == nil || *metric.ContainerResource.Target.AverageUtilization != 75 {
		t.Fatalf("unexpected utilization target: %+v", metric.ContainerResource.Target)
	}
	if hpa.Spec.Behavior == nil || hpa.Spec.Behavior.ScaleDown == nil ||
		hpa.Spec.Behavior.ScaleDown.SelectPolicy == nil ||
		*hpa.Spec.Behavior.ScaleDown.SelectPolicy != autoscalingv2.DisabledPolicySelect {
		t.Fatalf("scale-down is not disabled: %+v", hpa.Spec.Behavior)
	}
}

func TestRenderHorizontalPodAutoscalerRejectsMaxBelowMinimum(t *testing.T) {
	if _, err := RenderHorizontalPodAutoscaler("test-namespace", "login-test", 3, 2, 70); err == nil {
		t.Fatal("expected maxReplicas validation error")
	}
}

func TestRenderHorizontalPodAutoscalerRejectsInvalidTargetCPUUtilization(t *testing.T) {
	for _, target := range []int32{0, 101} {
		t.Run(fmt.Sprintf("target_%d", target), func(t *testing.T) {
			if _, err := RenderHorizontalPodAutoscaler("test-namespace", "login-test", 1, 2, target); err == nil {
				t.Fatal("expected targetCPUUtilizationPercentage validation error")
			}
		})
	}
}
