package login

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// RenderService renders new [corev1.Service] serving Slurm login
func RenderService(namespace, clusterName string, login *values.SlurmLogin) corev1.Service {
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      login.Service.Name,
			Namespace: namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeLogin, clusterName),
		},
		Spec: corev1.ServiceSpec{
			Type:     login.Service.Type,
			Selector: common.RenderMatchLabels(consts.ComponentTypeLogin, clusterName),
			Ports: []corev1.ServicePort{{
				Protocol:   login.Service.Protocol,
				Port:       login.ContainerSshd.Port,
				TargetPort: intstr.FromString(login.ContainerSshd.Name),
			}},
		},
	}
}
