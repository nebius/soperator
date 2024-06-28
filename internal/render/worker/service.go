package worker

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderService renders new [corev1.Service] serving Slurm workers
func RenderService(namespace, clusterName string, worker *values.SlurmWorker) corev1.Service {
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      worker.Service.Name,
			Namespace: namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeWorker, clusterName),
		},
		Spec: corev1.ServiceSpec{
			Type:      worker.Service.Type,
			Selector:  common.RenderMatchLabels(consts.ComponentTypeWorker, clusterName),
			ClusterIP: "None",
			Ports: []corev1.ServicePort{{
				Protocol:   worker.Service.Protocol,
				Port:       worker.ContainerSlurmd.Port,
				TargetPort: intstr.FromString(worker.ContainerSlurmd.Name),
			}},
		},
	}
}
