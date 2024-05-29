package controller

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/models/k8s"
	"nebius.ai/slurm-operator/internal/models/slurm"
)

// RenderService renders new [corev1.Service] serving Slurm controllers
//
// It exposes the following port:
//
// - [consts.ServiceControllerClusterPort]: the port at which controllers listen for workers
func RenderService(cluster smodels.ClusterValues) corev1.Service {
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Controller.Service.Name,
			Namespace: cluster.Controller.Service.Namespace,
			Labels:    k8smodels.BuildClusterDefaultLabels(cluster.Name, consts.ComponentTypeController),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: k8smodels.BuildClusterDefaultMatchLabels(cluster.Name, consts.ComponentTypeController),
			Ports:    cluster.Controller.Service.Ports,
		},
	}
}
