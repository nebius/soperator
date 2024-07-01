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
func RenderService(namespace, clusterName string, controller *values.SlurmController) corev1.Service {
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.Service.Name,
			Namespace: namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeController, clusterName),
		},
		Spec: corev1.ServiceSpec{
			Type:      controller.Service.Type,
			Selector:  common.RenderMatchLabels(consts.ComponentTypeController, clusterName),
			ClusterIP: "None",
			Ports: []corev1.ServicePort{{
				Protocol:   controller.Service.Protocol,
				Port:       controller.ContainerSlurmctld.Port,
				TargetPort: intstr.FromString(controller.ContainerSlurmctld.Name),
			}},
		},
	}
}
