package accounting

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderService renders new [corev1.Service] serving Slurm accounting
func RenderService(namespace, clusterName string, accounting *values.SlurmAccounting) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      accounting.Service.Name,
			Namespace: namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeAccounting, clusterName),
		},
		Spec: corev1.ServiceSpec{
			Type:      accounting.Service.Type,
			Selector:  common.RenderMatchLabels(consts.ComponentTypeAccounting, clusterName),
			ClusterIP: "",
			Ports: []corev1.ServicePort{{
				Protocol:   accounting.Service.Protocol,
				Port:       accounting.ContainerAccounting.Port,
				TargetPort: intstr.FromString(accounting.ContainerAccounting.Name),
			}},
		},
	}
}
