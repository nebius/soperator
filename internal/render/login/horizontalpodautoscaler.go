package login

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
)

func RenderHorizontalPodAutoscaler(
	namespace string,
	statefulSetName string,
	minReplicas int32,
	maxReplicas int32,
	targetCPUUtilizationPercentage int32,
) (autoscalingv2.HorizontalPodAutoscaler, error) {
	if maxReplicas < minReplicas {
		return autoscalingv2.HorizontalPodAutoscaler{}, fmt.Errorf(
			"login autoscaling maxReplicas (%d) must be at least minReplicas (%d)",
			maxReplicas,
			minReplicas,
		)
	}
	if targetCPUUtilizationPercentage < 1 || targetCPUUtilizationPercentage > 100 {
		return autoscalingv2.HorizontalPodAutoscaler{}, fmt.Errorf(
			"login autoscaling targetCPUUtilizationPercentage (%d) must be between 1 and 100",
			targetCPUUtilizationPercentage,
		)
	}

	disabledScaleDown := autoscalingv2.DisabledPolicySelect
	return autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulSetName,
			Namespace: namespace,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps.kruise.io/v1beta1",
				Kind:       "StatefulSet",
				Name:       statefulSetName,
			},
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ContainerResourceMetricSourceType,
					ContainerResource: &autoscalingv2.ContainerResourceMetricSource{
						Name:      corev1.ResourceCPU,
						Container: consts.ContainerNameSshd,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetCPUUtilizationPercentage,
						},
					},
				},
			},
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					SelectPolicy: &disabledScaleDown,
				},
			},
		},
	}, nil
}
