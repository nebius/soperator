package controller

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderService renders new [corev1.Service] serving Slurm controllers
//
// It exposes the following port:
//
// - [consts.ServiceControllerClusterPort]: the port at which controllers listen for workers
func RenderService(cluster *values.SlurmCluster) corev1.Service {
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.NodeController.Service.Name,
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeController, cluster.Name),
		},
		Spec: corev1.ServiceSpec{
			Type:     cluster.NodeController.Service.ServiceType,
			Selector: common.RenderMatchLabels(consts.ComponentTypeController, cluster.Name),
			Ports: []corev1.ServicePort{{
				Protocol: cluster.NodeController.Service.Protocol,
				Port:     cluster.NodeController.Service.Port,
			}},
		},
	}
}
