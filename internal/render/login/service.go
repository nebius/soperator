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
	res := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        login.Service.Name,
			Namespace:   namespace,
			Labels:      common.RenderLabels(consts.ComponentTypeLogin, clusterName),
			Annotations: login.Service.Annotations,
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

	switch login.Service.Type {
	case corev1.ServiceTypeLoadBalancer:
		if login.Service.LoadBalancerIP != "" {
			res.Spec.LoadBalancerIP = login.Service.LoadBalancerIP
		}
	case corev1.ServiceTypeNodePort:
		res.Spec.Ports[0].NodePort = login.Service.NodePort
	}

	return res
}

// RenderHeadlessService renders new headless [corev1.Service] for login pod-to-pod communication
func RenderHeadlessService(namespace, clusterName string, login *values.SlurmLogin) corev1.Service {
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      login.HeadlessService.Name,
			Namespace: namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeLogin, clusterName),
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			Selector:  common.RenderMatchLabels(consts.ComponentTypeLogin, clusterName),
			ClusterIP: "None",
			Ports: []corev1.ServicePort{{
				Protocol:   login.HeadlessService.Protocol,
				Port:       login.ContainerSshd.Port,
				TargetPort: intstr.FromString(login.ContainerSshd.Name),
			}},
		},
	}
}
