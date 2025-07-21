package controller

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderService renders new [corev1.Service] serving Slurm controllers
func RenderService(namespace, clusterName, svcName string, controller *values.SlurmController, podLabels ...map[string]string) corev1.Service {
	labels := common.RenderLabels(consts.ComponentTypeController, clusterName)

	selector := common.RenderMatchLabels(consts.ComponentTypeController, clusterName)
	for _, additionalLabels := range podLabels {
		for k, v := range additionalLabels {
			selector[k] = v
		}
	}

	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     controller.Service.Type,
			Selector: selector,
			Ports: []corev1.ServicePort{{
				Protocol:   controller.Service.Protocol,
				Port:       controller.ContainerSlurmctld.Port,
				TargetPort: intstr.FromString(controller.ContainerSlurmctld.Name),
			}},
		},
	}
}
